package tact

import (
	"encoding/json"
	"testing"
)

func TestGetKeyFound(t *testing.T) {
	kr := NewKeyRing()
	kr.AddKey("0123456789abcdef", "00112233445566778899aabbccddeeff")

	key, err := kr.GetKey("0123456789abcdef")
	if err != nil {
		t.Fatalf("expected key, got error: %v", err)
	}
	if key != "00112233445566778899aabbccddeeff" {
		t.Fatalf("wrong key: %s", key)
	}
}

func TestGetKeyCaseInsensitive(t *testing.T) {
	kr := NewKeyRing()
	kr.AddKey("0123456789ABCDEF", "00112233445566778899aabbccddeeff")

	key, err := kr.GetKey("0123456789abcdef")
	if err != nil {
		t.Fatalf("expected key, got error: %v", err)
	}
	if key != "00112233445566778899aabbccddeeff" {
		t.Fatalf("wrong key: %s", key)
	}
}

func TestGetKeyMissing(t *testing.T) {
	kr := NewKeyRing()

	_, err := kr.GetKey("0123456789abcdef")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestMissingKeyErrorType(t *testing.T) {
	kr := NewKeyRing()

	_, err := kr.GetKey("deadbeefdeadbeef")
	mke, ok := err.(*MissingKeyError)
	if !ok {
		t.Fatalf("expected MissingKeyError, got %T: %v", err, err)
	}
	if mke.KeyName != "deadbeefdeadbeef" {
		t.Fatalf("error keyName = %q", mke.KeyName)
	}
}

func TestAddKeyValidation(t *testing.T) {
	kr := NewKeyRing()

	if kr.AddKey("short", "00112233445566778899aabbccddeeff") {
		t.Fatal("should reject short keyName")
	}
	if kr.AddKey("0123456789abcdef", "short") {
		t.Fatal("should reject short key")
	}
	if !kr.AddKey("0123456789abcdef", "00112233445566778899aabbccddeeff") {
		t.Fatal("should accept valid key pair")
	}
}

func TestAddKeyDuplicateNoOverwrite(t *testing.T) {
	kr := NewKeyRing()
	kr.AddKey("0123456789abcdef", "00112233445566778899aabbccddeeff")
	kr.AddKey("0123456789abcdef", "ffeeddccbbaa99887766554433221100")

	key, _ := kr.GetKey("0123456789abcdef")
	// Node behavior: if keyName already has same key, skip; if different, overwrite
	if key != "ffeeddccbbaa99887766554433221100" {
		t.Fatalf("should overwrite with new key, got %s", key)
	}
}

func TestLoadKeysFromJSON(t *testing.T) {
	kr := NewKeyRing()
	data := map[string]string{
		"0123456789abcdef": "00112233445566778899aabbccddeeff",
		"deadbeefdeadbeef": "ffeeddccbbaa99887766554433221100",
	}
	jsonData, _ := json.Marshal(data)

	n, err := kr.LoadFromJSON(jsonData)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if n != 2 {
		t.Fatalf("loaded %d keys, want 2", n)
	}

	key, _ := kr.GetKey("0123456789abcdef")
	if key != "00112233445566778899aabbccddeeff" {
		t.Fatalf("wrong key: %s", key)
	}
}

func TestLoadKeysSkipsInvalid(t *testing.T) {
	kr := NewKeyRing()
	data := map[string]string{
		"invalid":           "00112233445566778899aabbccddeeff",
		"0123456789abcdef": "22334455667788990011223344556677",
		"deadbeefdeadbeef": "ffeeddccbbaa99887766554433221100",
	}
	jsonData, _ := json.Marshal(data)

	n, _ := kr.LoadFromJSON(jsonData)
	if n != 2 {
		t.Fatalf("loaded %d, want 2 (skipped 1 invalid)", n)
	}
}
