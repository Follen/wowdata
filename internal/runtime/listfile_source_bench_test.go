package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkListfileSmallObjectDownload(b *testing.B) {
	payload := []byte("small immutable listfile payload")
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(25 * time.Millisecond)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		if r.Method == http.MethodHead {
			return
		}
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(payload)-1, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := downloadListfileURLWithWorkers(server.Client(), server.URL, 2)
		if err != nil {
			b.Fatal(err)
		}
		if string(got) != string(payload) {
			b.Fatalf("payload = %q", got)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(requests.Load())/float64(b.N), "req/op")
}
