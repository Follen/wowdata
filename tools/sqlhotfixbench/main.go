package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"wowdata/internal/hotfix"
)

type target struct {
	Home    string `json:"home"`
	Path    string `json:"path"`
	Region  string `json:"region"`
	Product string `json:"product"`
	Build   string `json:"build"`
	Locale  string `json:"locale"`
}

type localCase struct {
	ID     string `json:"id"`
	Query  string `json:"query"`
	Format string `json:"format"`
}

type sample struct {
	Mode           string `json:"mode"`
	CacheSemantics string `json:"cache_semantics"`
	DurationNS     int64  `json:"duration_ns"`
	ExitStatus     int    `json:"exit_status"`
	StdoutBytes    int    `json:"stdout_bytes"`
	ResultHash     string `json:"result_hash,omitempty"`
	Error          string `json:"error,omitempty"`
}

type caseReport struct {
	ID         string   `json:"id"`
	Query      string   `json:"query"`
	Samples    []sample `json:"samples"`
	P50NS      int64    `json:"p50_ns"`
	P95NS      int64    `json:"p95_ns"`
	Equivalent bool     `json:"result_equivalent"`
}

type report struct {
	Schema      string       `json:"schema"`
	GeneratedAt time.Time    `json:"generated_at"`
	Mode        string       `json:"mode"`
	Target      target       `json:"target,omitempty"`
	Cases       []caseReport `json:"cases,omitempty"`
	Offline     any          `json:"offline,omitempty"`
	Live        any          `json:"live,omitempty"`
}

var cases = []localCase{
	{"point", "SELECT ID, Name_lang FROM SpellName WHERE ID=1", "json"},
	{"batch", "SELECT ID, Name_lang FROM SpellName WHERE ID IN (1,2,3) ORDER BY ID", "json"},
	{"relationship", "SELECT ID, SpellID, EffectIndex FROM SpellEffect WHERE SpellID=100 ORDER BY EffectIndex", "json"},
	{"projection", "SELECT ID, EffectIndex FROM SpellEffect WHERE ID IN (1,2,3)", "json"},
	{"selective-scan", "SELECT ID FROM SpellEffect WHERE EffectIndex=0 LIMIT 100", "json"},
	{"bounded-scan", "SELECT ID FROM SpellEffect LIMIT 100", "json"},
	{"join", "SELECT se.ID, sn.Name_lang FROM SpellEffect se JOIN SpellName sn ON sn.ID=se.SpellID WHERE se.ID IN (1,2,3) ORDER BY se.ID", "json"},
	{"aggregate", "SELECT SpellID, COUNT(*) AS n FROM SpellEffect WHERE ID IN (1,2,3,4,5,6,7,8) GROUP BY SpellID ORDER BY SpellID", "json"},
	{"distinct", "SELECT DISTINCT EffectIndex FROM SpellEffect WHERE ID IN (1,2,3,4,5,6,7,8) ORDER BY EffectIndex", "json"},
	{"top-k", "SELECT ID, EffectIndex FROM SpellEffect ORDER BY EffectIndex, ID DESC LIMIT 10", "json"},
	{"streaming", "SELECT ID, EffectIndex FROM SpellEffect WHERE EffectIndex=0 LIMIT 100", "jsonl"},
}

func main() {
	mode := flag.String("mode", "wago-offline", "local-functional, local-performance, wago-offline, or wago-live")
	binary := flag.String("binary", "wowdata.exe", "wowdata executable")
	home := flag.String("home", "", "WOWDATA_HOME")
	path := flag.String("path", "", "local WoW root")
	region := flag.String("region", "cn", "target region")
	product := flag.String("product", "wow", "target product")
	build := flag.String("build", "68887", "target Build")
	locale := flag.String("locale", "zhCN", "target locale")
	samplesN := flag.Int("samples", 10, "samples per local performance cell")
	manifest := flag.String("manifest", filepath.FromSlash("internal/hotfix/testdata/wago-hotfix-offline/manifest.json"), "offline Wago manifest")
	output := flag.String("output", "", "report path; stdout when empty")
	cache := flag.String("cache", "", "Wago live cache directory")
	flag.Parse()

	r := report{Schema: "wowdata.sql-hotfix-benchmark.v1", GeneratedAt: time.Now().UTC(), Mode: *mode}
	var err error
	switch *mode {
	case "local-functional", "local-performance":
		r.Target = target{*home, *path, *region, *product, *build, *locale}
		n := 1
		modes := []string{"functional"}
		if *mode == "local-performance" {
			n = *samplesN
			modes = []string{"independent-cold", "shared-build-cold", "warm", "repeat-warm"}
		}
		r.Cases, err = runLocal(*binary, r.Target, modes, n)
	case "wago-offline":
		r.Offline, err = runOffline(*manifest)
	case "wago-live":
		r.Live, err = runLive(*cache)
	default:
		err = fmt.Errorf("unknown mode %q", *mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	b = append(b, '\n')
	if *output == "" {
		_, _ = os.Stdout.Write(b)
		return
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(*output, b, 0644); err != nil {
		panic(err)
	}
}

func runLocal(binary string, t target, modes []string, samplesN int) ([]caseReport, error) {
	if t.Path == "" || t.Home == "" {
		return nil, fmt.Errorf("--home and --path are required")
	}
	var reports []caseReport
	for _, c := range cases {
		cr := caseReport{ID: c.ID, Query: c.Query, Equivalent: true}
		hashes := map[string]struct{}{}
		for _, mode := range modes {
			for i := 0; i < samplesN; i++ {
				s := runCommand(binary, t, c, mode)
				cr.Samples = append(cr.Samples, s)
				if s.ExitStatus != 0 {
					cr.Equivalent = false
				}
				if s.ResultHash != "" {
					hashes[s.ResultHash] = struct{}{}
				}
			}
		}
		cr.Equivalent = cr.Equivalent && len(hashes) == 1
		var durations []int64
		for _, s := range cr.Samples {
			if s.ExitStatus == 0 {
				durations = append(durations, s.DurationNS)
			}
		}
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		cr.P50NS, cr.P95NS = percentile(durations, .50), percentile(durations, .95)
		reports = append(reports, cr)
	}
	return reports, nil
}

func runCommand(binary string, t target, c localCase, mode string) sample {
	runHome := t.Home
	semantics := "persistent-home"
	cleanup := func() {}
	if mode == "independent-cold" {
		tmp, err := os.MkdirTemp("", "wowdata-sql-cold-")
		if err != nil {
			return sample{Mode: mode, CacheSemantics: "fresh-home", ExitStatus: 1, Error: err.Error()}
		}
		cleanup = func() { _ = os.RemoveAll(tmp) }
		if err = copyTree(t.Home, tmp); err != nil {
			cleanup()
			return sample{Mode: mode, CacheSemantics: "fresh-home", ExitStatus: 1, Error: err.Error()}
		}
		runHome = tmp
		semantics = "fresh-copy-of-frozen-home"
	} else if mode == "shared-build-cold" {
		semantics = "first-shared-home-wave"
	} else if mode == "warm" {
		semantics = "shared-home-warm"
	} else if mode == "repeat-warm" {
		semantics = "shared-home-repeat-warm"
	}
	defer cleanup()
	args := []string{"sql", c.Query, "--format", c.Format, "--source", "local", "--path", t.Path, "--region", t.Region, "--product", t.Product, "--build", t.Build, "--locale", t.Locale}
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), "WOWDATA_HOME="+runHome)
	started := time.Now()
	out, err := cmd.Output()
	s := sample{Mode: mode, CacheSemantics: semantics, DurationNS: time.Since(started).Nanoseconds(), StdoutBytes: len(out)}
	if err != nil {
		s.ExitStatus = 1
		s.Error = err.Error()
		if ee, ok := err.(*exec.ExitError); ok {
			s.ExitStatus = ee.ExitCode()
			s.Error = strings.TrimSpace(string(ee.Stderr))
		}
		return s
	}
	sum := sha256.Sum256(normalizeOutput(out, c.Format))
	s.ResultHash = hex.EncodeToString(sum[:])
	return s
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, info.Mode())
	})
}

func normalizeOutput(out []byte, format string) []byte {
	if format == "jsonl" {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		var rows []any
		for _, line := range lines {
			var envelope map[string]any
			if json.Unmarshal([]byte(line), &envelope) == nil {
				rows = append(rows, envelope["data"])
			}
		}
		b, _ := json.Marshal(rows)
		return b
	}
	var envelope map[string]any
	if json.Unmarshal(out, &envelope) != nil {
		return out
	}
	data, _ := envelope["data"].(map[string]any)
	b, _ := json.Marshal(data["rows"])
	return b
}

func percentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	i := int(float64(len(values)-1)*p + .5)
	if i >= len(values) {
		i = len(values) - 1
	}
	return values[i]
}

type offlineManifest struct {
	Schema string `json:"schema"`
	Items  []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
		Bytes  int64  `json:"bytes"`
	} `json:"items"`
	Cases []struct {
		ID    string `json:"id"`
		Path  string `json:"path"`
		Query struct {
			Product string `json:"product"`
			Build   string `json:"build"`
			Locale  string `json:"locale"`
			Table   string `json:"table"`
			Search  string `json:"search"`
			Region  uint32 `json:"region"`
			Page    int    `json:"page"`
		}
		Expect struct {
			Page          int   `json:"page"`
			LastPage      int   `json:"last_page"`
			Total         int64 `json:"total"`
			PostFilterMin int   `json:"post_filter_min"`
		}
	} `json:"cases"`
}

func runOffline(path string) (any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m offlineManifest
	if err = json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.Schema != "wowdata.hotfix-corpus-manifest.v1" {
		return nil, fmt.Errorf("unexpected schema %q", m.Schema)
	}
	for _, item := range m.Items {
		body, err := os.ReadFile(filepath.FromSlash(item.Path))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != item.SHA256 {
			return nil, fmt.Errorf("hash mismatch: %s", item.Path)
		}
	}
	results := make([]map[string]any, 0, len(m.Cases))
	for _, c := range m.Cases {
		raw, err := os.ReadFile(filepath.FromSlash(c.Path))
		if err != nil {
			return nil, err
		}
		q := hotfix.Query{Product: c.Query.Product, Build: c.Query.Build, Region: c.Query.Region, Locale: c.Query.Locale, Table: c.Query.Table, Search: c.Query.Search, Page: c.Query.Page}
		if q.Table == "" && q.Search == "" {
			q.Latest = true
		}
		res, meta, err := hotfix.ParseWagoHTML(raw, q)
		if err != nil {
			return nil, err
		}
		if meta.CurrentPage != c.Expect.Page || meta.LastPage != c.Expect.LastPage || (c.Expect.Total != 0 && meta.Total != c.Expect.Total) || len(res.Records) < c.Expect.PostFilterMin {
			return nil, fmt.Errorf("case %s expectation mismatch: got page=%d last=%d total=%d records=%d want page=%d last=%d total=%d min_records=%d", c.ID, meta.CurrentPage, meta.LastPage, meta.Total, len(res.Records), c.Expect.Page, c.Expect.LastPage, c.Expect.Total, c.Expect.PostFilterMin)
		}
		results = append(results, map[string]any{"id": c.ID, "records": len(res.Records), "page": meta.CurrentPage, "last_page": meta.LastPage, "total": meta.Total, "sha256": meta.ResponseSHA256})
	}
	return map[string]any{"manifest": path, "items": len(m.Items), "cases": results}, nil
}

func runLive(cache string) (any, error) {
	s := hotfix.NewWagoSource(nil).WithCache(cache)
	s.MaxPages = 1
	q := hotfix.Query{Product: "wow_classic_titan", Build: "3.80.2.69137", Region: 196, Locale: "zhCN", Table: "SpellPowerDifficulty", Search: "69137", Page: 1, Limit: 25}
	started := time.Now()
	res, err := s.Query(context.Background(), q)
	if err != nil {
		return nil, err
	}
	return map[string]any{"query": q, "duration_ns": time.Since(started).Nanoseconds(), "coverage": res.Coverage, "records": len(res.Records), "warnings": res.Warnings}, nil
}
