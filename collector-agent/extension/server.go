package extension

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"user-memory-collector/watchers"
)

// seenEntry tracks when a URL's content was last extracted to prevent duplicates
type seenEntry struct {
	extractedAt time.Time
	contentHash string
}

// ExtensionPayload mirrors the JSON body sent from background.js
type ExtensionPayload struct {
	URL        string      `json:"url"`
	Title      string      `json:"title"`
	DwellMs    int64       `json:"dwell_ms"`
	WebContent interface{} `json:"web_content"`
}

// Server acts as a localhost bridge to the Manifest V3 browser extension
type Server struct {
	server   *http.Server
	out      chan<- *watchers.Event
	mu       sync.Mutex
	seen     map[string]seenEntry
	seenTTL  time.Duration
	isPaused func() bool // agent pause state — skips dedup cache + event emit
}

func NewServer(out chan<- *watchers.Event, isPaused func() bool) *Server {
	s := &Server{
		out:      out,
		seen:     make(map[string]seenEntry),
		seenTTL:  4 * time.Hour,
		isPaused: isPaused,
	}
	go s.cleanupLoop()
	return s
}

func (s *Server) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/extension-event", s.handleEvent)

	s.server = &http.Server{
		Addr:    "127.0.0.1:45678",
		Handler: mux,
	}

	log.Println("Broker for Browser Extension attached at http://127.0.0.1:45678")
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Extension broker failed: %v", err)
	}
}

func (s *Server) Stop() {
	if s.server != nil {
		s.server.Close()
	}
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload ExtensionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Bad JSON", http.StatusBadRequest)
		return
	}

	// Always respond 200 so the browser extension doesn't retry
	w.WriteHeader(http.StatusOK)

	// When collection is paused: acknowledge the request but do NOT cache the URL
	// and do NOT emit the event. This ensures the URL stays re-capturable after resume.
	if s.isPaused != nil && s.isPaused() {
		log.Printf("[Extension] Paused — ignoring content for: %s", payload.URL)
		return
	}

	contentHash := s.hashPayload(payload)

	s.mu.Lock()
	entry, alreadySeen := s.seen[payload.URL]
	if alreadySeen && entry.contentHash == contentHash && time.Since(entry.extractedAt) < s.seenTTL {
		s.mu.Unlock()
		log.Printf("[Extension] Duplicate suppressed for: %s (%.0f min ago)",
			payload.URL, time.Since(entry.extractedAt).Minutes())
		return
	}
	s.seen[payload.URL] = seenEntry{
		extractedAt: time.Now(),
		contentHash: contentHash,
	}
	s.mu.Unlock()

	log.Printf("[Extension] New content accepted for: %s", payload.URL)

	rawPayload, _ := json.Marshal(payload)
	s.out <- &watchers.Event{
		EventID: "evt_url_" + time.Now().Format("20060102150405"),
		TsStart: time.Now().Add(-time.Duration(payload.DwellMs) * time.Millisecond),
		TsEnd:   time.Now(),
		Type:    watchers.EventURLVisit,
		Context: watchers.EventContext{
			WindowTitle: payload.Title,
			URL:         payload.URL,
			OSPlatform:  "windows",
		},
		Payload: rawPayload,
	}
}

// hashPayload creates a short fingerprint of the URL + content length combination.
// This detects if the page content has meaningfully changed (e.g. SPA navigation).
func (s *Server) hashPayload(p ExtensionPayload) string {
	raw, _ := json.Marshal(p.WebContent)
	h := sha256.Sum256(append([]byte(p.URL), raw[:min(len(raw), 512)]...))
	return fmt.Sprintf("%x", h[:8]) // 8 bytes = 64-bit fingerprint, sufficient for dedup
}

// cleanupLoop purges expired seen-entries every hour to prevent memory growth
func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		for url, entry := range s.seen {
			if time.Since(entry.extractedAt) > s.seenTTL {
				delete(s.seen, url)
			}
		}
		s.mu.Unlock()
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
