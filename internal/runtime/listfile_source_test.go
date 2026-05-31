package runtime

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestHTTPListfileSourceLoadsListfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("1;interface/icons/test.blp\n"))
	}))
	defer server.Close()

	source := NewHTTPListfileSource(t.TempDir(), []string{server.URL + "/community-listfile.csv"})
	lf, err := source.Listfile()
	if err != nil {
		t.Fatalf("Listfile: %v", err)
	}
	if got := lf.GetByID(1); got != "interface/icons/test.blp" {
		t.Fatalf("GetByID = %q", got)
	}
}

func TestHTTPListfileSourcePrefersCachedBinaryListfile(t *testing.T) {
	dir := t.TempDir()
	writeMinimalBinaryListfile(t, dir)
	source := NewHTTPListfileSource(dir, []string{"http://127.0.0.1:1/missing.csv"})

	lf, err := source.Listfile()
	if err != nil {
		t.Fatalf("Listfile: %v", err)
	}
	if got := lf.GetByID(7); got != "interface/icons/cached.blp" {
		t.Fatalf("GetByID = %q", got)
	}
}

func TestHTTPListfileSourceTextOnlyIgnoresCachedBinaryListfile(t *testing.T) {
	dir := t.TempDir()
	writeMinimalBinaryListfile(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "community-listfile.csv"), []byte("8;interface/icons/text.blp\n"), 0644); err != nil {
		t.Fatal(err)
	}
	source := NewHTTPListfileSource(dir, nil).WithBinaryURLs([]string{"http://127.0.0.1:1/%s"}).WithTextOnly()

	lf, err := source.Listfile()
	if err != nil {
		t.Fatalf("Listfile: %v", err)
	}
	if got := lf.GetByID(8); got != "interface/icons/text.blp" {
		t.Fatalf("GetByID(8) = %q", got)
	}
	if got := lf.GetByID(7); got != "" {
		t.Fatalf("text-only mode should ignore binary cache, got binary entry %q", got)
	}
}

func TestHTTPListfileSourceDownloadsBinaryComponentsConcurrently(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		mu.Lock()
		seen[name] = true
		mu.Unlock()
		switch name {
		case "listfile-id-index.dat":
			w.Write([]byte{0, 0, 0, 7, 0, 0, 0, 0, 0})
		case "listfile-strings.dat":
			w.Write([]byte("interface/icons/remote.blp\x00"))
		case "listfile-tree-nodes.dat":
			w.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0})
		default:
			w.Write([]byte{0, 0, 0, 0})
		}
	}))
	defer server.Close()

	source := NewHTTPListfileSource(t.TempDir(), nil).WithBinaryURLs([]string{server.URL + "/%s"})
	lf, err := source.Listfile()
	if err != nil {
		t.Fatalf("Listfile: %v", err)
	}
	if got := lf.GetByID(7); got != "interface/icons/remote.blp" {
		t.Fatalf("GetByID = %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, name := range []string{"listfile-id-index.dat", "listfile-strings.dat", "listfile-tree-nodes.dat"} {
		if !seen[name] {
			t.Fatalf("component %s was not downloaded; seen=%#v", name, seen)
		}
	}
}

func TestHTTPListfileSourceTextUsesRangeChunksForLargeCSV(t *testing.T) {
	body := bytes.Repeat([]byte("1;interface/icons/test.blp\n"), 50*1024)
	rangeHeaders := make([]string, 0)
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		rangeHeaders = append(rangeHeaders, r.Header.Get("Range"))
		mu.Unlock()
		serveRangeBody(t, w, r, body)
	}))
	defer server.Close()

	source := NewHTTPListfileSource(t.TempDir(), []string{server.URL + "/community-listfile.csv"}).WithTextOnly()
	if _, err := source.Listfile(); err != nil {
		t.Fatalf("Listfile: %v", err)
	}

	if got := countRangeRequests(rangeHeaders); got < 2 {
		t.Fatalf("expected large text listfile to use multiple range chunks, headers=%#v", rangeHeaders)
	}
}

func TestDownloadListfileURLReportsFallbackGETElapsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("1;interface/icons/test.blp\n"))
	}))
	defer server.Close()

	var stderr bytes.Buffer
	oldWriter := cascDownloadProgressWriter()
	setCASCDownloadProgressWriter(&stderr)
	defer setCASCDownloadProgressWriter(oldWriter)

	if _, err := downloadListfileURL(nil, server.URL+"/community-listfile.csv"); err != nil {
		t.Fatalf("downloadListfileURL: %v", err)
	}
	log := stderr.String()
	if !strings.Contains(log, "download") || !strings.Contains(log, "method=get") || !strings.Contains(log, "duration=") {
		t.Fatalf("download progress log = %q", log)
	}
}

func TestHTTPListfileSourceBinaryComponentUsesRangeChunksForLargeFile(t *testing.T) {
	stringComponent := bytes.Repeat([]byte("interface/icons/remote.blp\x00"), 50*1024)
	rangeHeadersByName := map[string][]string{}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		mu.Lock()
		rangeHeadersByName[name] = append(rangeHeadersByName[name], r.Header.Get("Range"))
		mu.Unlock()
		switch name {
		case "listfile-id-index.dat":
			w.Write([]byte{0, 0, 0, 7, 0, 0, 0, 0, 0})
		case "listfile-strings.dat":
			serveRangeBody(t, w, r, stringComponent)
		case "listfile-tree-nodes.dat":
			w.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0})
		default:
			w.Write([]byte{0, 0, 0, 0})
		}
	}))
	defer server.Close()

	source := NewHTTPListfileSource(t.TempDir(), nil).WithBinaryURLs([]string{server.URL + "/%s"})
	if _, err := source.Listfile(); err != nil {
		t.Fatalf("Listfile: %v", err)
	}

	mu.Lock()
	headers := append([]string(nil), rangeHeadersByName["listfile-strings.dat"]...)
	mu.Unlock()
	if got := countRangeRequests(headers); got < 2 {
		t.Fatalf("expected large binary component to use multiple range chunks, headers=%#v", headers)
	}
}

func serveRangeBody(t *testing.T, w http.ResponseWriter, r *http.Request, body []byte) {
	t.Helper()
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return
	}
	var start, end int
	if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
		t.Fatalf("bad range header %q: %v", rangeHeader, err)
	}
	if end >= len(body) {
		end = len(body) - 1
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(body[start : end+1])
}

func countRangeRequests(headers []string) int {
	count := 0
	for _, header := range headers {
		if strings.HasPrefix(header, "bytes=") {
			count++
		}
	}
	return count
}

func writeMinimalBinaryListfile(t *testing.T, dir string) {
	t.Helper()
	components := map[string][]byte{
		"listfile-id-index.dat":    {0, 0, 0, 7, 0, 0, 0, 0, 0},
		"listfile-strings.dat":     []byte("interface/icons/cached.blp\x00"),
		"listfile-tree-nodes.dat":  {0, 0, 0, 0, 0, 0, 0, 0, 0},
		"listfile-pf-models.dat":   {0, 0, 0, 0},
		"listfile-pf-textures.dat": {0, 0, 0, 0},
		"listfile-pf-sounds.dat":   {0, 0, 0, 0},
		"listfile-pf-videos.dat":   {0, 0, 0, 0},
		"listfile-pf-text.dat":     {0, 0, 0, 0},
		"listfile-pf-fonts.dat":    {0, 0, 0, 0},
	}
	for name, data := range components {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
