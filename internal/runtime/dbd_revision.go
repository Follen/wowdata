package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"wowdata/internal/casc"
	"wowdata/internal/resource"
	"wowdata/internal/storage"
)

var gitSHA1Pattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

const dbdRevisionTTL = time.Hour

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
	return &HTTPDBDRevisionSource{cacheDir: cacheDir, url: url, client: casc.InstrumentHTTPClient(&http.Client{Timeout: 15 * time.Second})}
}

func (s *HTTPDBDRevisionSource) WithStage(stage resource.StageID) *HTTPDBDRevisionSource {
	s.client = casc.InstrumentHTTPClientForStage(s.client, stage)
	return s
}

func (s *HTTPDBDRevisionSource) Revision() (string, error) {
	statePath := filepath.Join(s.cacheDir, "revision.json")
	state := DBDRevisionState{}
	if data, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	if checkedAt, err := time.Parse(time.RFC3339, state.CheckedAt); err == nil && time.Since(checkedAt) < dbdRevisionTTL && gitSHA1Pattern.MatchString(state.SHA) {
		return state.SHA, nil
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		req, requestErr := http.NewRequest(http.MethodGet, s.url, nil)
		if requestErr != nil {
			return "", requestErr
		}
		if state.ETag != "" {
			req.Header.Set("If-None-Match", state.ETag)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "wowdata")
		resp, requestErr := s.client.Do(req)
		err = requestErr
		if requestErr == nil {
			status := resp.StatusCode
			switch status {
			case http.StatusNotModified:
				_ = resp.Body.Close()
				if gitSHA1Pattern.MatchString(state.SHA) {
					state.CheckedAt = time.Now().UTC().Format(time.RFC3339)
					_ = storage.AtomicWriteJSON(statePath, state, 0o644)
					return state.SHA, nil
				}
			case http.StatusOK:
				var payload struct {
					SHA    string `json:"sha"`
					Object struct {
						SHA string `json:"sha"`
					} `json:"object"`
				}
				decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
				etag := resp.Header.Get("ETag")
				_ = resp.Body.Close()
				sha := payload.SHA
				if sha == "" {
					sha = payload.Object.SHA
				}
				if decodeErr == nil && gitSHA1Pattern.MatchString(sha) {
					state = DBDRevisionState{
						Schema: "wowdata.dbd-revision.v1", SHA: sha,
						ETag: etag, CheckedAt: time.Now().UTC().Format(time.RFC3339),
					}
					if writeErr := storage.AtomicWriteJSON(statePath, state, 0o644); writeErr != nil {
						return "", writeErr
					}
					return state.SHA, nil
				}
				err = fmt.Errorf("invalid WoWDBDefs revision response")
			default:
				_ = resp.Body.Close()
				err = fmt.Errorf("HTTP %d from %s", status, s.url)
			}
		}
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
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
