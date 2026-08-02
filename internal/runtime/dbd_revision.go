package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"wowdata/internal/storage"
)

var gitSHA1Pattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type DBDRevisionState struct {
	Schema    string `json:"schema"`
	SHA       string `json:"sha"`
	ETag      string `json:"etag,omitempty"`
	CheckedAt string `json:"checkedAt"`
}

type HTTPDBDRevisionSource struct {
	cacheDir string
	url      string
	client   *http.Client
}

func NewHTTPDBDRevisionSource(cacheDir, url string) *HTTPDBDRevisionSource {
	return &HTTPDBDRevisionSource{cacheDir: cacheDir, url: url, client: &http.Client{Timeout: 15 * time.Second}}
}

func (s *HTTPDBDRevisionSource) Revision() (string, error) {
	statePath := filepath.Join(s.cacheDir, "revision.json")
	state := DBDRevisionState{}
	if data, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	req, err := http.NewRequest(http.MethodGet, s.url, nil)
	if err != nil {
		return "", err
	}
	if state.ETag != "" {
		req.Header.Set("If-None-Match", state.ETag)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "wowdata")
	resp, err := s.client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusNotModified:
			if gitSHA1Pattern.MatchString(state.SHA) {
				state.CheckedAt = time.Now().UTC().Format(time.RFC3339)
				_ = storage.AtomicWriteJSON(statePath, state, 0o644)
				return state.SHA, nil
			}
		case http.StatusOK:
			var payload struct {
				SHA string `json:"sha"`
			}
			if decodeErr := json.NewDecoder(resp.Body).Decode(&payload); decodeErr == nil && gitSHA1Pattern.MatchString(payload.SHA) {
				state = DBDRevisionState{
					Schema: "wowdata.dbd-revision.v1", SHA: payload.SHA,
					ETag: resp.Header.Get("ETag"), CheckedAt: time.Now().UTC().Format(time.RFC3339),
				}
				if writeErr := storage.AtomicWriteJSON(statePath, state, 0o644); writeErr != nil {
					return "", writeErr
				}
				return state.SHA, nil
			}
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotModified {
			err = fmt.Errorf("HTTP %d from %s", resp.StatusCode, s.url)
		}
	}
	if gitSHA1Pattern.MatchString(state.SHA) {
		return state.SHA, nil
	}
	if err == nil {
		err = fmt.Errorf("invalid WoWDBDefs revision response")
	}
	return "", err
}
