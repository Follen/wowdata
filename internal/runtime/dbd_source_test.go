package runtime

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPDBDSourceFetchesDefinition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "SpellName.dbd") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte("COLUMNS\nint ID\n"))
	}))
	defer server.Close()

	source := NewHTTPDBDSource(t.TempDir(), []string{server.URL + "/%s.dbd"})
	def, err := source.Definition("SpellName")
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if !strings.Contains(def, "int ID") {
		t.Fatalf("definition = %q", def)
	}
}

func TestHTTPDBDSourceRefreshesCachedDefinition(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "SpellName.dbd")
	if err := os.WriteFile(cachePath, []byte("COLUMNS\nint Old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("COLUMNS\nint Current\n"))
	}))
	defer server.Close()

	source := NewHTTPDBDSource(cacheDir, []string{server.URL + "/%s.dbd"})
	definition, err := source.Definition("SpellName")
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if !strings.Contains(definition, "int Current") {
		t.Fatalf("definition = %q", definition)
	}
	cached, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(cached) != definition {
		t.Fatalf("cache = %q, definition = %q", cached, definition)
	}
}

func TestHTTPDBDSourceFallsBackToCachedDefinition(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "SpellName.dbd")
	if err := os.WriteFile(cachePath, []byte("COLUMNS\nint Cached\n"), 0644); err != nil {
		t.Fatal(err)
	}

	source := NewHTTPDBDSource(cacheDir, []string{"http://127.0.0.1:1/%s.dbd"})
	definition, err := source.Definition("SpellName")
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if !strings.Contains(definition, "int Cached") {
		t.Fatalf("definition = %q", definition)
	}
}
