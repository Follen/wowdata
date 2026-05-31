package tact

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type MissingKeyError struct {
	KeyName string
}

func (e *MissingKeyError) Error() string {
	return "[BLTE] Missing decryption key " + e.KeyName
}

type KeyRing struct {
	keys map[string]string // lowercase keyName -> lowercase key
}

func NewKeyRing() *KeyRing {
	return &KeyRing{keys: make(map[string]string)}
}

func (kr *KeyRing) GetKey(keyName string) (string, error) {
	key, ok := kr.keys[strings.ToLower(keyName)]
	if !ok {
		return "", &MissingKeyError{KeyName: strings.ToLower(keyName)}
	}
	return key, nil
}

func (kr *KeyRing) AddKey(keyName, key string) bool {
	if len(keyName) != 16 || len(key) != 32 {
		return false
	}
	kr.keys[strings.ToLower(keyName)] = strings.ToLower(key)
	return true
}

func (kr *KeyRing) Count() int {
	if kr == nil {
		return 0
	}
	return len(kr.keys)
}

func (kr *KeyRing) LoadFromJSON(data []byte) (int, error) {
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return 0, fmt.Errorf("tact keys: %w", err)
	}
	added := 0
	for keyName, key := range raw {
		if len(keyName) == 16 && len(key) == 32 {
			kr.keys[strings.ToLower(keyName)] = strings.ToLower(key)
			added++
		}
	}
	return added, nil
}

func (kr *KeyRing) LoadFromText(data []byte) int {
	added := 0
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		if kr.AddKey(parts[0], parts[1]) {
			added++
		}
	}
	return added
}

func LoadKeyRing(cachePath string, urls []string) (*KeyRing, error) {
	keyRing := NewKeyRing()
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 {
		_, _ = keyRing.LoadFromJSON(data)
	}
	var lastErr error
	for _, url := range urls {
		resp, err := http.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
			continue
		}
		if keyRing.LoadFromText(body) > 0 {
			_ = os.MkdirAll(filepath.Dir(cachePath), 0755)
			if data, err := json.MarshalIndent(keyRing.keys, "", "\t"); err == nil {
				_ = os.WriteFile(cachePath, data, 0644)
			}
			return keyRing, nil
		}
	}
	if len(keyRing.keys) > 0 {
		return keyRing, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no TACT key URLs configured")
	}
	return keyRing, lastErr
}
