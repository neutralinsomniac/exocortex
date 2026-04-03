package db

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// ForceFullSyncWith resets the push timestamp so that all local rows are
// sent to the server, then performs a normal sync. Use this after a server
// reset or when a peer is missing rows that were created before the last sync.
func (e *ExoDB) ForceFullSyncWith(serverURL, token string, encrypt bool) error {
	if err := e.SetSetting("last_push_ts", "0"); err != nil {
		return fmt.Errorf("reset push timestamp: %w", err)
	}
	if err := e.SetSetting("last_sync_ts", "0"); err != nil {
		return fmt.Errorf("reset sync timestamp: %w", err)
	}
	return e.SyncWith(serverURL, token, encrypt)
}

// SyncWith performs a bidirectional sync against the given server URL using
// the shared Bearer token. When encrypt is true the request and response
// bodies are AES-256-GCM encrypted using a key derived from the token; use
// this when TLS is not available. The local DB is updated in place; the caller
// should call State.Refresh() afterwards to pick up new data.
func (e *ExoDB) SyncWith(serverURL, token string, encrypt bool) error {
	// last_sync_ts: server's ServerTS from last response — filters what the
	// server sends back to us (we only want rows newer than our last pull).
	lastSyncStr, err := e.GetSetting("last_sync_ts")
	if err != nil {
		return fmt.Errorf("get last_sync_ts: %w", err)
	}
	var since int64
	if lastSyncStr != "" {
		if v, parseErr := strconv.ParseInt(lastSyncStr, 10, 64); parseErr == nil {
			since = v
		}
	}

	// last_push_ts: max updated_ts we successfully pushed last time — filters
	// what we send to the server (only rows created/modified since last push).
	// On first push (last_push_ts=0) we send everything, which correctly
	// bootstraps a fresh server. Thereafter only incremental rows are sent.
	lastPushStr, err := e.GetSetting("last_push_ts")
	if err != nil {
		return fmt.Errorf("get last_push_ts: %w", err)
	}
	var lastPush int64
	if lastPushStr != "" {
		if v, parseErr := strconv.ParseInt(lastPushStr, 10, 64); parseErr == nil {
			lastPush = v
		}
	}

	local, err := e.BuildSyncPayload(lastPush)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	// Overwrite Since so the server knows what to send back, independently of
	// what we're pushing.
	local.Since = since

	body, err := json.Marshal(local)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	contentType := "application/json"
	if encrypt {
		body, err = EncryptPayload(token, body)
		if err != nil {
			return fmt.Errorf("encrypt: %w", err)
		}
		contentType = "application/octet-stream"
	}

	req, err := http.NewRequest(http.MethodPost, serverURL+"/sync", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	// In TLS mode the header is protected by the transport layer.
	// In symmetric-encryption mode we skip the header entirely — a
	// successful AES-256-GCM decryption on the server already proves
	// knowledge of the token, so sending it in plaintext would defeat the
	// purpose.
	if token != "" && !encrypt {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %s: %s", resp.Status, bytes.TrimSpace(body))
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if encrypt {
		respBytes, err = DecryptPayload(token, respBytes)
		if err != nil {
			return fmt.Errorf("decrypt response: %w", err)
		}
	}

	var remote SyncPayload
	if err = json.Unmarshal(respBytes, &remote); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if err = e.ApplyChanges(remote); err != nil {
		return fmt.Errorf("apply remote changes: %w", err)
	}

	serverTS := remote.ServerTS
	if serverTS == 0 {
		serverTS = time.Now().UnixNano()
	}
	if err = e.SetSetting("last_sync_ts", strconv.FormatInt(serverTS, 10)); err != nil {
		return fmt.Errorf("save sync timestamp: %w", err)
	}

	// Advance last_push_ts to the highest updated_ts we just sent, so the
	// next sync only transmits rows modified after this one.
	var maxSentTS int64
	for _, r := range local.Rows {
		if r.UpdatedTS > maxSentTS {
			maxSentTS = r.UpdatedTS
		}
	}
	if maxSentTS > lastPush {
		if err = e.SetSetting("last_push_ts", strconv.FormatInt(maxSentTS, 10)); err != nil {
			return fmt.Errorf("save push timestamp: %w", err)
		}
	}

	return nil
}
