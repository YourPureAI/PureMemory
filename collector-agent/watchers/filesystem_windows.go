package watchers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"user-memory-collector/extractor"
)

// --- Windows API constants ---
const (
	fileShareAll              = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
	fileFlagBackupSemantics   = uint32(0x02000000)
	fileNotifyChangeLastWrite = uintptr(0x00000010)
	fileNotifyChangeFileName  = uintptr(0x00000001)
	fileActionAdded           = uint32(1)
	fileActionModified        = uint32(3)

	confirmDelaySec  = 10              // wait before extracting (spec requirement)
	editingDebounce  = 2 * time.Minute // inactivity after which editing session is concluded
	editingThreshold = 3               // number of MODIFIED events that signal active editing
	maxFileSizeBytes = int64(50 * 1024 * 1024) // 50MB per spec
)

var (
	kernel32Fs               = windows.NewLazySystemDLL("kernel32.dll")
	procReadDirectoryChangesW = kernel32Fs.NewProc("ReadDirectoryChangesW")
)

// fileNotifyInfo mirrors the Win32 FILE_NOTIFY_INFORMATION struct
type fileNotifyInfo struct {
	NextEntryOffset uint32
	Action          uint32
	FileNameLength  uint32
	FileName        [1]uint16
}

// fileState tracks per-file dedup and read/edit mode classification
type fileState struct {
	modTime     time.Time
	contentHash string // 8-byte SHA256 fingerprint for content-based dedup
	editing     bool
	modCount    int
	debounce    *time.Timer
}

// Blacklisted extensions — never extract content from these
var fsExtBlacklist = map[string]bool{
	".key": true, ".pem": true, ".p12": true, ".pfx": true, ".cer": true,
	".kdbx": true, ".wallet": true, ".env": true,
}

// watchedDirs returns the specific directories to monitor.
// Watching only these 3 instead of the entire home dir keeps CPU near zero.
func watchedDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, "Documents"),
		filepath.Join(home, "Desktop"),
		filepath.Join(home, "Downloads"),
	}
}

// FilesystemWatcher monitors file changes and classifies them as read or edit
type FilesystemWatcher struct {
	mu     sync.Mutex
	seen   map[string]*fileState // case-normalized path → state
	out    chan<- *Event
	appCtx func() EventContext // callback returning current active app
}

func NewFilesystemWatcher(appCtx func() EventContext) *FilesystemWatcher {
	return &FilesystemWatcher{
		seen:   make(map[string]*fileState),
		appCtx: appCtx,
	}
}

// Start launches one goroutine per watched directory (minimal footprint)
func (w *FilesystemWatcher) Start(ctx context.Context, out chan<- *Event) {
	w.out = out
	for _, dir := range watchedDirs() {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		log.Printf("[FS] Watching: %s", dir)
		go w.watchDir(ctx, dir)
	}
}

func (w *FilesystemWatcher) watchDir(ctx context.Context, dir string) {
	dirPtr, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return
	}

	handle, err := windows.CreateFile(
		dirPtr,
		windows.FILE_LIST_DIRECTORY,
		fileShareAll,
		nil,
		windows.OPEN_EXISTING,
		fileFlagBackupSemantics,
		0,
	)
	if err != nil {
		log.Printf("[FS] Cannot open dir handle for %s: %v", dir, err)
		return
	}
	defer windows.CloseHandle(handle)

	buf := make([]byte, 32*1024) // 32KB — single OS-level buffer, reused every call

	for {
		if ctx.Err() != nil {
			return
		}

		var bytesReturned uint32
		r, _, callErr := procReadDirectoryChangesW.Call(
			uintptr(handle),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			1, // bWatchSubtree = TRUE
			fileNotifyChangeLastWrite|fileNotifyChangeFileName,
			uintptr(unsafe.Pointer(&bytesReturned)),
			0, // lpOverlapped = NULL (synchronous blocking call)
			0, // lpCompletionRoutine = NULL
		)
		if r == 0 {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[FS] ReadDirectoryChangesW error in %s: %v", filepath.Base(dir), callErr)
			time.Sleep(2 * time.Second)
			continue
		}

		w.parseEvents(buf[:bytesReturned], dir)
	}
}

func (w *FilesystemWatcher) parseEvents(buf []byte, baseDir string) {
	for offset := 0; offset < len(buf); {
		if offset+12 > len(buf) {
			break
		}
		info := (*fileNotifyInfo)(unsafe.Pointer(&buf[offset]))

		nameSlice := (*[32768]uint16)(unsafe.Pointer(&buf[offset+12]))
		nameLen := int(info.FileNameLength / 2)
		if nameLen > 32768 {
			break
		}
		name := windows.UTF16ToString(nameSlice[:nameLen])
		fullPath := filepath.Join(baseDir, name)

		go w.handleFileEvent(fullPath, info.Action)

		if info.NextEntryOffset == 0 {
			break
		}
		offset += int(info.NextEntryOffset)
	}
}

func (w *FilesystemWatcher) handleFileEvent(path string, action uint32) {
	ext := strings.ToLower(filepath.Ext(path))
	if fsExtBlacklist[ext] {
		return
	}

	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "~$") {
		return // Skip hidden/temp files
	}

	normPath := strings.ToLower(path)

	w.mu.Lock()
	state, exists := w.seen[normPath]
	if !exists {
		state = &fileState{}
		w.seen[normPath] = state
	}

	if action == fileActionModified {
		state.modCount++
		if state.modCount >= editingThreshold && !state.editing {
			state.editing = true
			log.Printf("[FS] Editing session: %s", base)
		}

		if state.editing {
			// Debounce: reset the 2-min extraction timer on every write
			if state.debounce != nil {
				state.debounce.Stop()
			}
			capturedPath := path
			capturedState := state
			state.debounce = time.AfterFunc(editingDebounce, func() {
				w.extractAndEmit(capturedPath, capturedState, "document_edited")
			})
			w.mu.Unlock()
			return
		}
	}
	isEditing := state.editing
	w.mu.Unlock()

	// Reading mode: confirm after delay that same app is still active
	if !isEditing && (action == fileActionAdded || action == fileActionModified) {
		capturedPath := path
		capturedNorm := normPath
		capturedState := state
		time.AfterFunc(confirmDelaySec*time.Second, func() {
			w.mu.Lock()
			s := w.seen[capturedNorm]
			w.mu.Unlock()
			if s != nil && s.editing {
				return // File transitioned to edit mode during delay — skip read
			}
			w.extractAndEmit(capturedPath, capturedState, "document_read")
		})
	}
}

func (w *FilesystemWatcher) extractAndEmit(path string, state *fileState, entryType string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}

	// Fast dedup: skip if modTime unchanged and we have a previous hash
	w.mu.Lock()
	if state.modTime.Equal(info.ModTime()) && state.contentHash != "" {
		log.Printf("[FS] Skip (unchanged modTime): %s", filepath.Base(path))
		state.editing = false
		state.modCount = 0
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	if info.Size() > maxFileSizeBytes {
		log.Printf("[FS] Skip (too large): %s", filepath.Base(path))
		return
	}

	ext := strings.ToLower(filepath.Ext(path))
	var result *extractor.ExtractResult

	switch {
	case extractor.IsPlainText(ext):
		result, err = extractor.ReadText(path)
	case ext == ".pdf":
		result, err = extractor.ExtractPDF(path)
	default:
		result = &extractor.ExtractResult{Method: "metadata_only", Extracted: false}
		err = nil
	}
	if err != nil {
		log.Printf("[FS] Extraction error %s: %v", filepath.Base(path), err)
		return
	}

	// Content-hash dedup: skip if text body didn't actually change
	w.mu.Lock()
	if result.Hash != "" && result.Hash == state.contentHash {
		log.Printf("[FS] Skip (same hash): %s", filepath.Base(path))
		state.modTime = info.ModTime() // update modTime to avoid future redundant reads
		state.editing = false
		state.modCount = 0
		w.mu.Unlock()
		return
	}
	state.modTime = info.ModTime()
	state.contentHash = result.Hash
	state.editing = false
	state.modCount = 0
	w.mu.Unlock()

	log.Printf("[FS] %s — %s, %d chars (%s)", entryType, filepath.Base(path), result.CharCount, result.Method)

	payload, _ := json.Marshal(map[string]interface{}{
		"path":       path,
		"name":       filepath.Base(path),
		"extension":  ext,
		"size_bytes": info.Size(),
		"content": map[string]interface{}{
			"extracted":  result.Extracted,
			"method":     result.Method,
			"text":       result.Text,
			"entry_type": entryType,
		},
	})

	appCtx := w.appCtx()
	w.out <- &Event{
		EventID: fmt.Sprintf("evt_file_%d", time.Now().UnixMilli()),
		TsStart: time.Now(),
		TsEnd:   time.Now(),
		Type:    EventFileAccess,
		Context: appCtx,
		Payload: payload,
	}
}
