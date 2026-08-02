package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestDBDRevisionUsesETagAndCachedSHA(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests > 1 && r.Header.Get("If-None-Match") == `"revision"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"revision"`)
		fmt.Fprintf(w, `{"sha":%q}`, sha)
	}))
	defer server.Close()

	source := NewHTTPDBDRevisionSource(filepath.Join(t.TempDir(), "dbd"), server.URL)
	for i := 0; i < 2; i++ {
		got, err := source.Revision()
		if err != nil || got != sha {
			t.Fatalf("Revision %d = %q, %v", i, got, err)
		}
	}
}
