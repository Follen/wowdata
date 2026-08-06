// Command processprobe measures OS and Go startup for the exact wowdata binary.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

const schema = "wowdata.process-start-calibration.v1"

type report struct {
	Schema       string  `json:"schema"`
	GeneratedAt  string  `json:"generatedAt"`
	Binary       string  `json:"binary"`
	BinarySHA256 string  `json:"binarySha256"`
	Probe        string  `json:"probe"`
	Samples      []int64 `json:"samplesNanos"`
	MinNanos     int64   `json:"minNanos"`
	P50Nanos     int64   `json:"p50Nanos"`
	P95Nanos     int64   `json:"p95Nanos"`
	MaxNanos     int64   `json:"maxNanos"`
}

func main() {
	binary := flag.String("binary", "", "wowdata binary to probe (required)")
	output := flag.String("output", "", "write JSON report to this path (stdout when empty)")
	samples := flag.Int("samples", 10, "number of process launches")
	flag.Parse()
	if *binary == "" || *samples < 1 || *samples > 1000 {
		fatal(errors.New("-binary and -samples between 1 and 1000 are required"))
	}
	abs, err := filepath.Abs(*binary)
	if err != nil {
		fatal(err)
	}
	digest, err := fileSHA256(abs)
	if err != nil {
		fatal(err)
	}
	durations := make([]int64, 0, *samples)
	for i := 0; i < *samples; i++ {
		command := exec.Command(abs)
		command.Env = append(os.Environ(), "WOWDATA_PROCESS_START_PROBE=1")
		started := time.Now()
		if err := command.Run(); err != nil {
			fatal(fmt.Errorf("sample %d: %w", i+1, err))
		}
		durations = append(durations, time.Since(started).Nanoseconds())
	}
	result := summarize(abs, digest, durations)
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatal(err)
	}
	encoded = append(encoded, '\n')
	if *output == "" {
		_, err = os.Stdout.Write(encoded)
	} else {
		if err = os.MkdirAll(filepath.Dir(*output), 0o755); err == nil {
			err = os.WriteFile(*output, encoded, 0o644)
		}
	}
	if err != nil {
		fatal(err)
	}
}

func summarize(binary, digest string, samples []int64) report {
	sorted := append([]int64(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return report{
		Schema: schema, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Binary: filepath.ToSlash(binary), BinarySHA256: digest,
		Probe:   "launch exact binary with WOWDATA_PROCESS_START_PROBE=1; process exits at main entry before CLI construction",
		Samples: samples, MinNanos: sorted[0], P50Nanos: percentile(sorted, 0.50),
		P95Nanos: percentile(sorted, 0.95), MaxNanos: sorted[len(sorted)-1],
	}
}

func percentile(sorted []int64, q float64) int64 {
	index := int(float64(len(sorted)-1)*q + 0.5)
	return sorted[index]
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "processprobe:", err)
	os.Exit(1)
}
