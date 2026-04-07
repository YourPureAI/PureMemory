package extractor

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
)

const maxReadBytes = 50 * 1024 // 50KB per spec

var plainTextExtensions = map[string]bool{
	".txt": true, ".md": true, ".rst": true, ".csv": true, ".log": true,
	".py": true, ".js": true, ".go": true, ".ts": true, ".java": true,
	".c": true, ".cpp": true, ".h": true, ".sh": true, ".yaml": true,
	".yml": true, ".json": true, ".toml": true, ".xml": true, ".sql": true,
}

// ExtractResult holds extracted text and metadata
type ExtractResult struct {
	Text      string
	Method    string
	Extracted bool
	CharCount int
	Hash      string // SHA256 fingerprint of content for dedup
}

// IsPlainText returns true if the file extension is a known plain text format
func IsPlainText(ext string) bool {
	return plainTextExtensions[strings.ToLower(ext)]
}

// ReadText reads up to 50KB from a plain text file and returns a fingerprint
func ReadText(path string) (*ExtractResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Read only up to maxReadBytes to stay memory-minimal
	buf := make([]byte, maxReadBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	buf = buf[:n]

	text := string(buf)
	h := sha256.Sum256(buf)

	return &ExtractResult{
		Text:      text,
		Method:    "plaintext",
		Extracted: true,
		CharCount: len(text),
		Hash:      fmt.Sprintf("%x", h[:8]), // 64-bit fingerprint for dedup
	}, nil
}
