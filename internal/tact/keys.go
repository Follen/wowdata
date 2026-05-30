package tact

import (
	"encoding/json"
	"fmt"
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
