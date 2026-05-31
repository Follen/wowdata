package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPDBDManifestSourceFetchesManifest(t *testing.T) {
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
}
