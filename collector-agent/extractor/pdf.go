package extractor

import (
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const pdfMaxChars = 10000 // per spec

// findPdfToText searches for the pdftotext binary in standard Windows locations
func findPdfToText() string {
	candidates := []string{
		filepath.Join(os.Getenv("PROGRAMFILES"), "poppler", "bin", "pdftotext.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "poppler", "bin", "pdftotext.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	// Also check PATH
	if p, err := exec.LookPath("pdftotext"); err == nil {
		return p
	}
	return ""
}

// ExtractPDF runs pdftotext on the given path; gracefully falls back if tool missing
func ExtractPDF(path string) (*ExtractResult, error) {
	bin := findPdfToText()
	if bin == "" {
		log.Printf("[Extractor] pdftotext not found, skipping content for: %s", filepath.Base(path))
		return &ExtractResult{Method: "skipped_no_tool", Extracted: false}, nil
	}

	// Run subprocess: pdftotext <path> - (stdout output)
	out, err := exec.Command(bin, path, "-").Output()
	if err != nil {
		return &ExtractResult{Method: "skipped_error", Extracted: false}, err
	}

	// Truncate to spec limit
	text := string(out)
	if len(text) > pdfMaxChars {
		text = text[:pdfMaxChars]
	}
	text = strings.TrimSpace(text)

	h := sha256.Sum256([]byte(text))
	return &ExtractResult{
		Text:      text,
		Method:    "pdftotext",
		Extracted: true,
		CharCount: len(text),
		Hash:      fmt.Sprintf("%x", h[:8]),
	}, nil
}
