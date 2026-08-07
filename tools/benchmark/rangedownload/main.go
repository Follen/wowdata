package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wowdata/internal/casc"
	"wowdata/internal/resource"
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
	var maxMiB int
	var timeout time.Duration
	flag.StringVar(&url, "url", "", "range-capable object URL")
	flag.StringVar(&output, "output", "", "atomic publication path")
	flag.StringVar(&workdir, "workdir", "", "resume state directory")
	flag.StringVar(&expectedSHA256, "sha256", "", "expected payload SHA-256")
	flag.IntVar(&workers, "workers", 0, "large-range workers (0 uses the production point-query plan)")
	flag.IntVar(&chunkMiB, "chunk-mib", 4, "resume chunk size in MiB")
	flag.IntVar(&maxMiB, "max-mib", 16, "reject objects larger than this before range download")
	flag.DurationVar(&timeout, "timeout", 4*time.Minute+30*time.Second, "hard download and publication deadline")
	flag.Parse()

	if url == "" || output == "" || workdir == "" || expectedSHA256 == "" {
		fatalf("url, output, workdir, and sha256 are required")
	}
	if workers < 0 || chunkMiB < 1 || chunkMiB > 64 || maxMiB < 1 || maxMiB > 4096 || timeout <= 0 || timeout > 5*time.Minute {
		fatalf("workers must be non-negative, chunk-mib must be 1..64, max-mib must be 1..4096, and timeout must be positive and at most 5m")
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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := casc.DownloadHTTPConcurrentResumableWithOptions(url, partPath, statePath, casc.ResumeOptions{
		Workers: workers, TTL: casc.DefaultResumeTTL, Scheduler: scheduler,
		ChunkSize: int64(chunkMiB) * resource.MiB, ObjectMaxBytes: int64(maxMiB) * resource.MiB, Context: ctx, KeepPart: true,
	})
	if err != nil {
		fatalf("download: %v", err)
	}
	part, err := os.Open(partPath)
	if err != nil {
		fatalf("open completed part: %v", err)
	}
	h := sha256.New()
	bytes, hashErr := io.CopyBuffer(h, part, make([]byte, 256<<10))
	closeErr := part.Close()
	if hashErr != nil {
		fatalf("hash completed part: %v", hashErr)
	}
	if closeErr != nil {
		fatalf("close completed part: %v", closeErr)
	}
	actualSHA256 := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actualSHA256, expectedSHA256) {
		fatalf("SHA-256 mismatch: got %s want %s", actualSHA256, expectedSHA256)
	}
	if err := publishPart(partPath, output); err != nil {
		fatalf("publish: %v", err)
	}

	network := casc.SnapshotHTTPMetrics()
	fmt.Fprintf(os.Stderr, "network metrics=%s\n", mustJSON(network))
	response := result{
		Schema: "wowdata.range-download-calibration.v1", Bytes: int(bytes),
		SHA256: actualSHA256, Published: true,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		fatalf("encode result: %v", err)
	}
}

func publishPart(partPath, output string) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	if err := os.Rename(partPath, output); err == nil {
		return nil
	}
	if err := os.Remove(output); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(partPath, output)
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
