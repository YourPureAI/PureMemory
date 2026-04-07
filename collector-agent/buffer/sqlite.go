package buffer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
	"user-memory-collector/watchers"
)

type SQLiteBuffer struct {
	db *sql.DB
}

func NewSQLiteBuffer(dsn string) (*SQLiteBuffer, error) {
	connStr := fmt.Sprintf("%s?cache=shared&mode=rwc&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dsn)
	
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	
	schema := `
	CREATE TABLE IF NOT EXISTS events (
		id          TEXT PRIMARY KEY,
		created_at  INTEGER NOT NULL,
		payload     TEXT NOT NULL,
		sent        INTEGER DEFAULT 0,
		sent_at     INTEGER
	);
	CREATE INDEX IF NOT EXISTS idx_events_sent ON events(sent, created_at);
	`
	_, err = db.Exec(schema)
	if err != nil {
		return nil, err
	}

	return &SQLiteBuffer{db: db}, nil
}

func (b *SQLiteBuffer) SaveEvent(ev *watchers.Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	query := `INSERT INTO events (id, created_at, payload, sent) VALUES (?, ?, ?, 0)`
	_, err = b.db.Exec(query, ev.EventID, ev.TsStart.UnixMilli(), string(payload))
	if err != nil {
		return err
	}
	return nil
}

// GetPendingBatch retrieves up to 'limit' unsent events
func (b *SQLiteBuffer) GetPendingBatch(limit int) ([]*watchers.Event, error) {
	query := `SELECT payload FROM events WHERE sent = 0 ORDER BY created_at ASC LIMIT ?`
	rows, err := b.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*watchers.Event
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		var ev watchers.Event
		if err := json.Unmarshal([]byte(raw), &ev); err == nil {
			events = append(events, &ev)
		}
	}
	return events, nil
}

// AckBatch marks a slice of event IDs as successfully sent
func (b *SQLiteBuffer) AckBatch(ids []string, sentAtMs int64) error {
	if len(ids) == 0 {
		return nil
	}
	// Pure DB optimization avoiding N roundtrips
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = sentAtMs
	for i, id := range ids {
		placeholders[i] = "?"
		args[i+1] = id
	}
	
	query := fmt.Sprintf(`UPDATE events SET sent = 1, sent_at = ? WHERE id IN (%s)`, strings.Join(placeholders, ","))
	_, err := b.db.Exec(query, args...)
	return err
}

func (b *SQLiteBuffer) Close() error {
	return b.db.Close()
}
