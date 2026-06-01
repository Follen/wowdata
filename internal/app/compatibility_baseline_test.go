package app

import (
	"encoding/json"
	"testing"
)

func TestResponseEnvelopeKeysStayStable(t *testing.T) {
	resp := NewSuccessResponse("query rows", map[string]interface{}{"rows": []interface{}{}})
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	for _, key := range []string{"ok", "command", "data", "warnings"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("response envelope missing key %q in %#v", key, decoded)
		}
	}
	if decoded["ok"] != true {
		t.Fatalf("ok = %#v, want true", decoded["ok"])
	}
}

func TestErrorEnvelopeKeysStayStable(t *testing.T) {
	resp := NewErrorResponse("warmup", "not_ready", "CASC 未就绪，请先调用 wow_warmup")
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	errObj, ok := decoded["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error object missing in %#v", decoded)
	}
	if errObj["code"] != "not_ready" {
		t.Fatalf("error.code = %#v, want not_ready", errObj["code"])
	}
}
