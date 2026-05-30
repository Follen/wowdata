package app

import (
	"encoding/json"
	"testing"
)

func TestResponseEnvelopeSuccess(t *testing.T) {
	resp := NewSuccessResponse("wowdata test", map[string]any{"value": 42})

	if !resp.OK {
		t.Fatalf("expected OK response")
	}
	if resp.Command != "wowdata test" {
		t.Fatalf("command = %q", resp.Command)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %#v", resp.Error)
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	for _, key := range []string{"ok", "command", "data", "warnings"} {
		if _, exists := decoded[key]; !exists {
			t.Fatalf("missing envelope key %q in %#v", key, decoded)
		}
	}
}

func TestResponseEnvelopeError(t *testing.T) {
	resp := NewErrorResponse("wowdata bad", "not_implemented", "command is not implemented yet")

	if resp.OK {
		t.Fatalf("expected non-OK response")
	}
	if resp.Error == nil {
		t.Fatalf("expected error payload")
	}
	if resp.Error.Code != "not_implemented" {
		t.Fatalf("error code = %q", resp.Error.Code)
	}
}
