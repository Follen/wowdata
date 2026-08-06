package runtime

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"wowdata/internal/casc"
)

func TestHTTPDBDManifestSourceFetchesManifest(t *testing.T) {
	casc.ResetHTTPMetrics()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"tableName":"SpellName","db2FileDataID":123}]`))
	}))
	defer server.Close()

	source := NewHTTPDBDManifestSource(t.TempDir(), []string{server.URL + "/manifest.json"})
	manifest, err := source.Manifest()
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	id, ok := manifest.GetByTableName("SpellName")
	if !ok || id != 123 {
		t.Fatalf("SpellName = %d %v", id, ok)
	}
	metrics := casc.SnapshotHTTPMetrics()
	if metrics.Requests != 1 || metrics.ResponseBytes == 0 || metrics.UniquePayloadBytes != metrics.ResponseBytes {
		t.Fatalf("HTTP metrics = %#v", metrics)
	}
}

func TestHTTPDBDManifestSourceRefreshesCachedManifest(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "dbd-manifest.json")
	if err := os.WriteFile(cachePath, []byte(`[{"tableName":"SpellName","db2FileDataID":100}]`), 0644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"tableName":"SpellName","db2FileDataID":200}]`))
	}))
	defer server.Close()

	source := NewHTTPDBDManifestSource(cacheDir, []string{server.URL + "/manifest.json"})
	manifest, err := source.Manifest()
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if id, ok := manifest.GetByTableName("SpellName"); !ok || id != 200 {
		t.Fatalf("SpellName = %d %v", id, ok)
	}
	cached, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(cached) != `[{"tableName":"SpellName","db2FileDataID":200}]` {
		t.Fatalf("cache = %q", cached)
	}
}

func TestHTTPDBDManifestSourceFallsBackToCachedManifest(t *testing.T) {
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "dbd-manifest.json")
	if err := os.WriteFile(cachePath, []byte(`[{"tableName":"SpellName","db2FileDataID":300}]`), 0644); err != nil {
		t.Fatal(err)
	}

	source := NewHTTPDBDManifestSource(cacheDir, []string{"http://127.0.0.1:1/manifest.json"})
	manifest, err := source.Manifest()
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if id, ok := manifest.GetByTableName("SpellName"); !ok || id != 300 {
		t.Fatalf("SpellName = %d %v", id, ok)
	}
}
