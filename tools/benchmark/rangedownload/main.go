package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"wowdata/internal/casc"
	"wowdata/internal/resource"
	"wowdata/internal/storage"
)

type result struct {
	Schema    string `json:"schema"`
	Bytes     int    `json:"bytes"`
	SHA256    string `json:"sha256"`
	Published bool   `json:"published"`
}

func main() {
	var url string
	var output string
	var workdir string
	var expectedSHA256 string
	var workers int
	var chunkMiB int
	flag.StringVar(&url, "url", "", "range-capable object URL")
	flag.StringVar(&output, "output", "", "atomic publication path")
	flag.StringVar(&workdir, "workdir", "", "resume state directory")
	flag.StringVar(&expectedSHA256, "sha256", "", "expected payload SHA-256")
	flag.IntVar(&workers, "workers", 0, "large-range workers (0 uses the production point-query plan)")
	flag.IntVar(&chunkMiB, "chunk-mib", 4, "resume chunk size in MiB")
	flag.Parse()

	if url == "" || output == "" || workdir == "" || expectedSHA256 == "" {
		fatalf("url, output, workdir, and sha256 are required")
	}
	if workers < 0 || chunkMiB < 1 || chunkMiB > 64 {
		fatalf("workers must be non-negative and chunk-mib must be between 1 and 64")
	}
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		fatalf("create workdir: %v", err)
	}

	plan := resource.NewPlanWithPoolOverrides(resource.PointQuery, 0, 1, workers)
	if workers == 0 {
		workers = plan.LargeRangeWorkers
	}
	scheduler := resource.NewScheduler(plan)
	partPath := filepath.Join(workdir, "payload.part")
	statePath := filepath.Join(workdir, "payload.state.json")
	casc.ResetHTTPMetrics()
	data, err := casc.DownloadHTTPConcurrentResumableWithOptions(url, partPath, statePath, casc.ResumeOptions{
		Workers: workers, TTL: casc.DefaultResumeTTL, Scheduler: scheduler,
		ChunkSize: int64(chunkMiB) * resource.MiB,
	})
	if err != nil {
		fatalf("download: %v", err)
	}
	digest := sha256.Sum256(data)
	actualSHA256 := hex.EncodeToString(digest[:])
	if !strings.EqualFold(actualSHA256, expectedSHA256) {
		fatalf("SHA-256 mismatch: got %s want %s", actualSHA256, expectedSHA256)
	}
	if err := storage.AtomicWriteFile(output, data, 0o644); err != nil {
		fatalf("publish: %v", err)
	}

	network := casc.SnapshotHTTPMetrics()
	fmt.Fprintf(os.Stderr, "network metrics=%s\n", mustJSON(network))
	response := result{
		Schema: "wowdata.range-download-calibration.v1", Bytes: len(data),
		SHA256: actualSHA256, Published: true,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		fatalf("encode result: %v", err)
	}
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		fatalf("encode metrics: %v", err)
	}
	return string(data)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
