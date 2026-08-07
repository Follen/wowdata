package hotfix

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func wagoHTML(component string, current, last, total int, records string) []byte {
	j := `{"component":"` + component + `","props":{"hotfixes":{"current_page":` + itoa(current) + `,"last_page":` + itoa(last) + `,"per_page":25,"total":` + itoa(total) + `,"data":` + records + `,"next_page_url":"/hotfixes?page=2"}}}`
	return []byte(`<html><body><div id="app" data-page="` + html.EscapeString(j) + `"></div></body></html>`)
}
func itoa(v int) string {
	const d = "0123456789"
	if v == 0 {
		return "0"
	}
	s := ""
	for v > 0 {
		s = string(d[v%10]) + s
		v /= 10
	}
	return s
}
func TestParseWagoHTMLExactPostFilter(t *testing.T) {
	raw := wagoHTML("Hotfixes", 1, 2, 2, `[{"id":1,"push_id":"100","record_id":55,"status":1,"build":"68914","table_name":"SpellPowerDifficulty","data":null,"created_at":"2026-08-05 22:09:07","region_id":196,"locale":"zhCN","search_text":"68943"},{"id":2,"push_id":101,"record_id":"55","status":"3","build":68943,"table_name":"SpellPowerDifficulty","data":["7",[1,2]],"created_at":"2026-08-05 22:09:08","region_id":"196","locale":"zhCN","search_text":"68943"}]`)
	q := Query{Product: "wow_classic_titan", Build: "3.80.2.68943", Region: 196, Locale: "zhCN", Table: "SpellPowerDifficulty", RecordID: u32(55), Search: "68943"}
	r, m, err := ParseWagoHTML(raw, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Records) != 1 || r.Records[0].ID != 2 || m.ResponseSHA256 == "" || len(m.EmbeddedJSON) == 0 {
		t.Fatalf("result=%#v meta=%#v", r, m)
	}
	if r.Coverage.Complete {
		t.Fatal("multi-page response marked complete")
	}
}
func TestParseWagoHTMLProtocolChanged(t *testing.T) {
	q := Query{Product: "wow", Build: "69137", Region: 196, Locale: "zhCN", Latest: true}
	for name, raw := range map[string][]byte{"component": wagoHTML("Other", 1, 1, 0, `[]`), "missing": []byte(`<div id="app"></div>`)} {
		_, _, err := ParseWagoHTML(raw, q)
		if err == nil || !strings.Contains(err.Error(), "hotfix_protocol_changed") {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
func TestValidatePageSequence(t *testing.T) {
	a := WagoPageMeta{CurrentPage: 1, LastPage: 3, Total: 60, NextPageURL: "/hotfixes?page=2"}
	if err := ValidatePageSequence(a, WagoPageMeta{CurrentPage: 2, LastPage: 3, Total: 60}); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePageSequence(a, WagoPageMeta{CurrentPage: 3, LastPage: 3, Total: 60}); err == nil {
		t.Fatal("missing page accepted")
	}
	if err := ValidatePageSequence(a, WagoPageMeta{CurrentPage: 2, LastPage: 4, Total: 60}); err == nil {
		t.Fatal("drift accepted")
	}
	bad := a
	bad.NextPageURL = "/hotfixes?page=9"
	if err := ValidatePageSequence(bad, WagoPageMeta{CurrentPage: 2, LastPage: 3, Total: 60}); err == nil {
		t.Fatal("discontinuous next URL accepted")
	}
}

func TestWagoSourceCacheRepeatWarmAndCorruption(t *testing.T) {
	var calls atomic.Int32
	raw := wagoHTML("Hotfixes", 1, 1, 1, `[{"id":2,"push_id":101,"record_id":55,"status":1,"build":68943,"table_name":"SpellPowerDifficulty","data":null,"created_at":"2026-08-05 22:09:08","region_id":196,"locale":"zhCN","search_text":"55"}]`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(raw)
	}))
	defer server.Close()
	cache := t.TempDir()
	source := NewWagoSource(server.Client()).WithCache(cache)
	source.BaseURL = server.URL
	q := Query{Product: "wow_classic_titan", Build: "3.80.2.68943", Region: 196, Locale: "zhCN", Table: "SpellPowerDifficulty", RecordID: u32(55), Page: 1}
	first, err := source.Query(context.Background(), q)
	if err != nil || len(first.Records) != 1 || calls.Load() != 1 {
		t.Fatalf("first=%#v calls=%d err=%v", first, calls.Load(), err)
	}
	second, err := source.Query(context.Background(), q)
	if err != nil || len(second.Records) != 1 || calls.Load() != 1 || !contains(second.Warnings, "wago_cache_hit") {
		t.Fatalf("second=%#v calls=%d err=%v", second, calls.Load(), err)
	}
	htmlFiles, _ := filepath.Glob(filepath.Join(cache, "*.html"))
	if len(htmlFiles) != 1 {
		t.Fatalf("cache files=%v", htmlFiles)
	}
	if err := os.WriteFile(htmlFiles[0], []byte("corrupt"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Query(context.Background(), q); err != nil || calls.Load() != 2 {
		t.Fatalf("corrupt refetch calls=%d err=%v", calls.Load(), err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
