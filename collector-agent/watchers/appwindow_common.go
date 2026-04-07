package watchers

import (
	"context"
	"encoding/json"
	"time"
)

// EventType categorizes the captured telemetry
type EventType string

const (
	EventAppFocus   EventType = "app_focus"
	EventURLVisit   EventType = "url_visit"
	EventTextInput  EventType = "text_input"
	EventClipboard  EventType = "clipboard"
	EventFileAccess EventType = "file_access"
	EventIdleStart  EventType = "idle_start"
	EventIdleEnd    EventType = "idle_end"
	EventQuickNote  EventType = "quick_note" // User-initiated thought capture via hotkey
)

// Event is the envelope for all telemetry sent to the pipeline
type Event struct {
	EventID       string          `json:"event_id"`         // ULID (Mocked as string)
	UserID        string          `json:"user_id"`
	SessionID     string          `json:"session_id"`       // UUID per boot/login
	FocusID       string          `json:"focus_session_id"` // UUID per app focus
	DeviceID      string          `json:"device_id"`        // HMAC(machine-id, api_key)
	SchemaVersion string          `json:"schema_version"`   // "1.0"
	TsStart       time.Time       `json:"ts_start"`
	TsEnd         time.Time       `json:"ts_end"`
	ActiveMs      int64           `json:"active_duration_ms"`
	IdleMs        int64           `json:"idle_duration_ms"`
	Timezone      string          `json:"tz"`
	Type          EventType       `json:"type"`
	Context       EventContext    `json:"context"`
	Payload       json.RawMessage `json:"payload"` // Type-specific payload
}

// EventContext provides application context for strict privacy filtering
type EventContext struct {
	AppBundle   string `json:"app_bundle"` // E.g., Chrome.exe
	AppName     string `json:"app_name"`
	WindowTitle string `json:"window_title"`
	URL         string `json:"url,omitempty"`
	URLDomain   string `json:"url_domain,omitempty"`
	OSPlatform  string `json:"os_platform"` // "windows"
}

// TextInputPayload carries typing data
type TextInputPayload struct {
	Text       string `json:"text"`
	WordCount  int    `json:"word_count"`
	DurationMs int64  `json:"duration_ms"`
}

// ClipboardPayload carries copy/paste metadata
type ClipboardPayload struct {
	ContentType string `json:"content_type"` // "text" | "skipped"
	Text        string `json:"text,omitempty"`
	CharCount   int    `json:"char_count"`
}

// QuickNotePayload carries user-initiated text and context relationship
type QuickNotePayload struct {
	Note          string   `json:"note"`
	ContextType   string   `json:"context_type"` // "standard", "clipboard", "file"
	ClipboardText string   `json:"clipboard_text,omitempty"`
	FilePaths     []string `json:"file_paths,omitempty"`
}



// Watcher is a generic interface for OS-level metric collectors
type Watcher interface {
	Start(ctx context.Context, out chan<- *Event)
}
