package db

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// SyncWith performs a bidirectional sync against the given server URL using
// the shared Bearer token. The local DB is updated in place; the caller
// should call State.Refresh() afterwards to pick up new data.
func (e *ExoDB) SyncWith(serverURL, token string) error {
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

	local, err := e.BuildSyncPayload(since)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}

	body, err := json.Marshal(local)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, serverURL+"/sync", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %s", resp.Status)
	}

	var remote SyncPayload
	if err = json.NewDecoder(resp.Body).Decode(&remote); err != nil {
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

	return nil
}
