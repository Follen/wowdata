package runtime

import (
	"net/http"
	"net/http/httptest"
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
