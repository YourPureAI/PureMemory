package transport

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"user-memory-collector/buffer"
	"user-memory-collector/watchers"
)

type BatchPayload struct {
	BatchID  string            `json:"batch_id"`
	UserID   string            `json:"user_id"`
	DeviceID string            `json:"device_id"`
	Events   []*watchers.Event `json:"events"`
}

type ServerResponse struct {
	Status  string   `json:"status"`
	BatchID string   `json:"batch_id"`
	AckIDs  []string `json:"ack_ids"`
}

type Client struct {
	ServerURL string
	APIKey    string
	Buffer    *buffer.SQLiteBuffer
	UserID    string
	DeviceID  string
}

func NewClient(serverURL, apiKey, userID, deviceID string, buf *buffer.SQLiteBuffer) *Client {
	return &Client{
		ServerURL: serverURL,
		APIKey:    apiKey,
		Buffer:    buf,
		UserID:    userID,
		DeviceID:  deviceID,
	}
}

func (c *Client) StartLoop(ctx context.Context) {
	log.Printf("Network Transport initialized to %s\n", c.ServerURL)
	ticker := time.NewTicker(60 * time.Second) // Flush every 60s locally for testing
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.flush(); err != nil {
				log.Printf("Transport flush failed: %v", err)
			}
		}
	}
}

func (c *Client) flush() error {
	events, err := c.Buffer.GetPendingBatch(100)
	if err != nil || len(events) == 0 {
		return err
	}

	batch := BatchPayload{
		BatchID:  "batch_" + time.Now().Format("20060102150405"),
		UserID:   c.UserID,
		DeviceID: c.DeviceID,
		Events:   events,
	}

	rawJSON, _ := json.Marshal(batch)

	// GZIP Compression as per spec
	var gzipBuf bytes.Buffer
	writer := gzip.NewWriter(&gzipBuf)
	writer.Write(rawJSON)
	writer.Close()

	req, err := http.NewRequest("POST", c.ServerURL+"/api/v1/events", &gzipBuf)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err // Implicit backoff since we just retry next tick
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		respBody, _ := ioutil.ReadAll(resp.Body)
		var apiResp ServerResponse
		if err := json.Unmarshal(respBody, &apiResp); err == nil {
			// Mark Ack'd events
			c.Buffer.AckBatch(apiResp.AckIDs, time.Now().UnixMilli())
			log.Printf("Successfully flushed %d events to server.", len(apiResp.AckIDs))
		}
	} else {
		return fmt.Errorf("Server returned non-200 status: %d", resp.StatusCode)
	}

	return nil
}
