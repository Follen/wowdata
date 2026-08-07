// Command networkcalibrate records interleaved network probes and scheduler evidence.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const schema = "wowdata.network-calibration.v1"

type config struct {
	Output, CDNMetadataURL, CDNRangeURL, GitHubURL, DBDURL    string
	MetadataTournament, LargeRangeTournament, ChunkTournament string
	Rounds, RangeMiB, MultiConnections                        int
	Timeout, TotalTimeout                                     time.Duration
	MaxRequests                                               int
	MaxResponseBytes                                          int64
}

type report struct {
	Schema      string          `json:"schema"`
	GeneratedAt string          `json:"generatedAt"`
	Protocol    protocol        `json:"protocol"`
	Samples     []sample        `json:"samples"`
	Sources     []sourceSummary `json:"sources"`
	Selection   selection       `json:"selection"`
}
type protocol struct {
	Rounds           int      `json:"rounds"`
	RangeBytes       int      `json:"rangeBytes"`
	MultiConnections int      `json:"multiConnections"`
	Timeout          string   `json:"timeout"`
	TotalTimeout     string   `json:"totalTimeout"`
	MaxRequests      int      `json:"maxRequests"`
	MaxResponseBytes int64    `json:"maxResponseBytes"`
	OrderPolicy      string   `json:"orderPolicy"`
	CachePolicy      string   `json:"cachePolicy"`
	Sources          []string `json:"sources"`
}
type sample struct {
	Round             int            `json:"round"`
	Order             int            `json:"order"`
	Name              string         `json:"name"`
	URL               string         `json:"url"`
	Mode              string         `json:"mode"`
	StartedAt         string         `json:"startedAt"`
	Status            string         `json:"status"`
	Error             string         `json:"error,omitempty"`
	HTTPStatuses      []int          `json:"httpStatuses,omitempty"`
	Protocols         map[string]int `json:"protocols,omitempty"`
	Requests          int            `json:"requests"`
	WarmupRequests    int            `json:"warmupRequests,omitempty"`
	WarmupBytes       int64          `json:"warmupBytes,omitempty"`
	Connections       int            `json:"connections"`
	ReusedConnections int            `json:"reusedConnections"`
	ResponseBytes     int64          `json:"responseBytes"`
	WallNanos         int64          `json:"wallNanos"`
	TTFBNanos         int64          `json:"ttfbNanos,omitempty"`
	DNSNanos          int64          `json:"dnsNanos,omitempty"`
	ConnectNanos      int64          `json:"connectNanos,omitempty"`
	TLSNanos          int64          `json:"tlsNanos,omitempty"`
	BodyNanos         int64          `json:"bodyNanos,omitempty"`
	BytesPerSecond    float64        `json:"bytesPerSecond,omitempty"`
	SHA256            string         `json:"sha256,omitempty"`
	Ranges            []string       `json:"ranges,omitempty"`
}
type sourceSummary struct {
	Name              string         `json:"name"`
	Successful        int            `json:"successful"`
	Failed            int            `json:"failed"`
	P50WallNanos      int64          `json:"p50WallNanos,omitempty"`
	P95WallNanos      int64          `json:"p95WallNanos,omitempty"`
	P50TTFBNanos      int64          `json:"p50TtfbNanos,omitempty"`
	P95TTFBNanos      int64          `json:"p95TtfbNanos,omitempty"`
	P50DNSNanos       int64          `json:"p50DnsNanos,omitempty"`
	P50ConnectNanos   int64          `json:"p50ConnectNanos,omitempty"`
	P50TLSNanos       int64          `json:"p50TlsNanos,omitempty"`
	P50BytesPerSecond float64        `json:"p50BytesPerSecond,omitempty"`
	P95BytesPerSecond float64        `json:"p95BytesPerSecond,omitempty"`
	Protocols         map[string]int `json:"protocols"`
}
type selection struct {
	Metadata   candidateDecision `json:"metadata"`
	LargeRange candidateDecision `json:"largeRange"`
	Chunk      candidateDecision `json:"chunk"`
	Network    networkDecision   `json:"network"`
}
type candidateDecision struct {
	Initial    int                 `json:"initial"`
	Unit       string              `json:"unit"`
	Source     string              `json:"source"`
	Candidates []tournamentSummary `json:"candidates,omitempty"`
	Retained   []int               `json:"retained"`
	Eliminated []eliminated        `json:"eliminated"`
	Rule       string              `json:"rule"`
}
type networkDecision struct {
	SingleBps              float64 `json:"singleBytesPerSecond"`
	MultiBps               float64 `json:"multiBytesPerSecond"`
	MultiSpeedup           float64 `json:"multiSpeedup"`
	RTTP50Ms               float64 `json:"rttEstimateP50Ms"`
	RTTEstimateMethod      string  `json:"rttEstimateMethod"`
	BDPBytes               float64 `json:"bdpBytes"`
	RecommendedConnections int     `json:"recommendedConnections"`
	Rule                   string  `json:"rule"`
}
type eliminated struct {
	Candidate int    `json:"candidate"`
	Reason    string `json:"reason"`
}
type tournamentSummary struct {
	Candidate              int     `json:"candidate"`
	SuccessfulRuns         int     `json:"successfulRuns"`
	FailedAttempts         int     `json:"failedAttempts"`
	P50WallMs              float64 `json:"p50WallMs"`
	P95WallMs              float64 `json:"p95WallMs"`
	P50ArchiveMs           float64 `json:"p50ArchiveMs"`
	P95ArchiveMs           float64 `json:"p95ArchiveMs"`
	MeanRequests           float64 `json:"meanRequests"`
	MeanCanceledRequests   float64 `json:"meanCanceledRequests"`
	MaxPeakWorkingSetBytes float64 `json:"maxPeakWorkingSetBytes"`
}
type tournamentReport struct {
	Summary []tournamentSummary `json:"summary"`
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Output, "output", "analyze/benchmark/network-calibration/report.json", "output JSON")
	flag.StringVar(&cfg.CDNMetadataURL, "cdn-metadata-url", "https://cn.version.battlenet.com.cn/wow/versions", "CN CDN metadata URL")
	flag.StringVar(&cfg.CDNRangeURL, "cdn-range-url", "https://blzdist-wow.necdn.leihuo.netease.com/tpr/wow/data/c7/5e/c75eab4959595b210498861537fd02e2", "CN CDN immutable object URL")
	flag.StringVar(&cfg.GitHubURL, "github-url", "https://api.github.com/repos/wowdev/WoWDBDefs/commits/master", "GitHub revision URL")
	flag.StringVar(&cfg.DBDURL, "dbd-url", "https://raw.githubusercontent.com/wowdev/WoWDBDefs/master/definitions/SpellName.dbd", "raw DBD URL")
	flag.StringVar(&cfg.MetadataTournament, "metadata-tournament", "analyze/benchmark/metadata-tournament-r12-10x-rotating-tail64/report.json", "metadata tournament report")
	flag.StringVar(&cfg.LargeRangeTournament, "large-range-tournament", "analyze/benchmark/large-range-tournament-r36-demand-final/report.json", "on-demand large-range tournament report")
	flag.StringVar(&cfg.ChunkTournament, "chunk-tournament", "analyze/benchmark/chunk-tournament-r13-10x-rotating-tail64-meta24/report.json", "chunk tournament report")
	flag.IntVar(&cfg.Rounds, "rounds", 2, "interleaved rounds")
	flag.IntVar(&cfg.RangeMiB, "range-mib", 4, "MiB per CDN throughput sample")
	flag.IntVar(&cfg.MultiConnections, "multi-connections", 4, "parallel disjoint CDN ranges")
	flag.DurationVar(&cfg.Timeout, "timeout", 20*time.Second, "per probe timeout")
	flag.DurationVar(&cfg.TotalTimeout, "total-timeout", 4*time.Minute+30*time.Second, "hard suite deadline (maximum 5m)")
	flag.IntVar(&cfg.MaxRequests, "max-requests", 96, "hard HTTP request budget")
	flag.Int64Var(&cfg.MaxResponseBytes, "max-response-bytes", 256<<20, "hard response byte budget")
	flag.Parse()
	r, err := run(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "networkcalibrate:", err)
		os.Exit(1)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(cfg.Output), 0755)
	}
	if err == nil {
		err = os.WriteFile(cfg.Output, append(data, '\n'), 0644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "networkcalibrate:", err)
		os.Exit(1)
	}
}

func run(cfg config) (report, error) {
	if cfg.Rounds < 1 || cfg.Rounds > 20 || cfg.RangeMiB < 1 || cfg.RangeMiB > 64 || cfg.MultiConnections < 1 || cfg.MultiConnections > 16 || cfg.Timeout <= 0 || cfg.TotalTimeout <= 0 || cfg.TotalTimeout > 5*time.Minute || cfg.MaxRequests < 1 || cfg.MaxResponseBytes < 1 {
		return report{}, errors.New("rounds=1..20, range-mib=1..64, multi-connections=1..16, positive probe timeout, total-timeout<=5m, and positive request/byte budgets required")
	}
	suiteCtx, cancel := context.WithTimeout(context.Background(), cfg.TotalTimeout)
	defer cancel()
	specs := []probeSpec{
		{"cdn-metadata", cfg.CDNMetadataURL, "metadata", 0, 1, false},
		{"github-revision", cfg.GitHubURL, "metadata", 0, 1, false},
		{"dbd-raw", cfg.DBDURL, "metadata", 0, 1, false},
		{"cdn-single", cfg.CDNRangeURL, "range-single", int64(cfg.RangeMiB) << 20, 1, false},
		{"cdn-multi", cfg.CDNRangeURL, "range-multi", int64(cfg.RangeMiB) << 20, cfg.MultiConnections, false},
		{"cdn-small-4", cfg.CDNRangeURL, "range-small-shared", 4 * 128 << 10, 4, true},
		{"cdn-small-24", cfg.CDNRangeURL, "range-small-shared", 24 * 64 << 10, 24, true},
	}
	var samples []sample
	requests := 0
	responseBytes := int64(0)
	for round := 0; round < cfg.Rounds; round++ {
		for order := range specs {
			if err := suiteCtx.Err(); err != nil {
				return report{}, fmt.Errorf("network calibration deadline: %w", err)
			}
			if requests >= cfg.MaxRequests || responseBytes >= cfg.MaxResponseBytes {
				return report{}, fmt.Errorf("network calibration budget exhausted: requests=%d/%d bytes=%d/%d", requests, cfg.MaxRequests, responseBytes, cfg.MaxResponseBytes)
			}
			s := probe(suiteCtx, cfg.Timeout, round+1, order+1, specs[(order+round)%len(specs)])
			samples = append(samples, s)
			requests += s.Requests + s.WarmupRequests
			responseBytes += s.ResponseBytes + s.WarmupBytes
			if requests > cfg.MaxRequests || responseBytes > cfg.MaxResponseBytes {
				return report{}, fmt.Errorf("network calibration budget exceeded: requests=%d/%d bytes=%d/%d", requests, cfg.MaxRequests, responseBytes, cfg.MaxResponseBytes)
			}
		}
	}
	sources := summarize(samples)
	metadata, err := loadTournament(cfg.MetadataTournament)
	if err != nil {
		return report{}, fmt.Errorf("metadata tournament: %w", err)
	}
	largeRange, err := loadTournament(cfg.LargeRangeTournament)
	if err != nil {
		return report{}, fmt.Errorf("large-range tournament: %w", err)
	}
	chunk, err := loadTournament(cfg.ChunkTournament)
	if err != nil {
		return report{}, fmt.Errorf("chunk tournament: %w", err)
	}
	p := protocol{Rounds: cfg.Rounds, RangeBytes: cfg.RangeMiB << 20, MultiConnections: cfg.MultiConnections, Timeout: cfg.Timeout.String(), TotalTimeout: cfg.TotalTimeout.String(), MaxRequests: cfg.MaxRequests, MaxResponseBytes: cfg.MaxResponseBytes, OrderPolicy: "rotating-latin", Sources: []string{cfg.CDNMetadataURL, cfg.CDNRangeURL, cfg.GitHubURL, cfg.DBDURL}, CachePolicy: "fresh Transport per sample; shared small-range probes warm the Transport before the measured steady-state window; Cache-Control no-cache; disjoint immutable CDN ranges; payload retained only in memory"}
	return report{Schema: schema, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Protocol: p, Samples: samples, Sources: sources, Selection: decide(metadata, largeRange, chunk, sources, cfg)}, nil
}

type probeSpec struct {
	name, url, mode string
	bytes           int64
	connections     int
	shared          bool
}
type traceResult struct {
	status, proto, contentRange   string
	bytes                         int64
	ttfb, dns, connect, tls, body time.Duration
	reused                        bool
	hash                          string
	err                           error
}

func probe(parent context.Context, timeout time.Duration, round, order int, spec probeSpec) sample {
	s := sample{Round: round, Order: order, Name: spec.name, URL: spec.url, Mode: spec.mode, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Status: "pass", Protocols: map[string]int{}}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	count := spec.connections
	if spec.mode == "metadata" {
		count = 1
	}
	results := make([]traceResult, count)
	var sharedTransport *http.Transport
	var sharedClient *http.Client
	if spec.shared {
		sharedTransport = &http.Transport{ForceAttemptHTTP2: true, MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1}
		sharedClient = &http.Client{Transport: sharedTransport}
		warm := requestWithClient(ctx, sharedClient, spec.url, spec.bytes, spec.bytes)
		if warm.err != nil {
			s.Status = "fail"
			s.Error = "shared transport warmup: " + warm.err.Error()
			sharedTransport.CloseIdleConnections()
			return s
		}
		s.WarmupRequests = 1
		s.WarmupBytes = warm.bytes
		defer sharedTransport.CloseIdleConnections()
	}
	started := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			start, end := int64(-1), int64(-1)
			if spec.bytes > 0 {
				part := spec.bytes / int64(count)
				start = int64(index) * part
				end = start + part - 1
				if index == count-1 {
					end = spec.bytes - 1
				}
			}
			if sharedClient != nil {
				results[index] = requestWithClient(ctx, sharedClient, spec.url, start, end)
			} else {
				results[index] = request(ctx, spec.url, start, end)
			}
		}(i)
	}
	wg.Wait()
	s.WallNanos = time.Since(started).Nanoseconds()
	h := sha256.New()
	for _, r := range results {
		if r.err != nil {
			s.Status = "fail"
			if s.Error == "" {
				s.Error = r.err.Error()
			}
		}
		fields := strings.Fields(r.status)
		if len(fields) > 0 {
			code, _ := strconv.Atoi(fields[0])
			s.HTTPStatuses = append(s.HTTPStatuses, code)
		}
		if r.proto != "" {
			s.Protocols[r.proto]++
		}
		s.Requests++
		if r.reused {
			s.ReusedConnections++
		} else {
			s.Connections++
		}
		s.ResponseBytes += r.bytes
		s.TTFBNanos = max64(s.TTFBNanos, r.ttfb.Nanoseconds())
		s.DNSNanos = max64(s.DNSNanos, r.dns.Nanoseconds())
		s.ConnectNanos = max64(s.ConnectNanos, r.connect.Nanoseconds())
		s.TLSNanos = max64(s.TLSNanos, r.tls.Nanoseconds())
		s.BodyNanos = max64(s.BodyNanos, r.body.Nanoseconds())
		if r.contentRange != "" {
			s.Ranges = append(s.Ranges, r.contentRange)
		}
		decoded, _ := hex.DecodeString(r.hash)
		h.Write(decoded)
	}
	if s.WallNanos > 0 {
		s.BytesPerSecond = float64(s.ResponseBytes) / (float64(s.WallNanos) / 1e9)
	}
	s.SHA256 = hex.EncodeToString(h.Sum(nil))
	sort.Ints(s.HTTPStatuses)
	sort.Strings(s.Ranges)
	return s
}

func request(ctx context.Context, rawURL string, start, end int64) traceResult {
	transport := &http.Transport{ForceAttemptHTTP2: true, DisableKeepAlives: true}
	client := &http.Client{Transport: transport}
	defer transport.CloseIdleConnections()
	return requestWithClient(ctx, client, rawURL, start, end)
}

func requestWithClient(ctx context.Context, client *http.Client, rawURL string, start, end int64) traceResult {
	var r traceResult
	var requestStart, dnsStart, connectStart, tlsStart, firstByte time.Time
	trace := &httptrace.ClientTrace{DNSStart: func(httptrace.DNSStartInfo) { dnsStart = time.Now() }, DNSDone: func(httptrace.DNSDoneInfo) {
		if !dnsStart.IsZero() {
			r.dns += time.Since(dnsStart)
		}
	}, ConnectStart: func(_, _ string) { connectStart = time.Now() }, ConnectDone: func(_, _ string, _ error) {
		if !connectStart.IsZero() {
			r.connect += time.Since(connectStart)
		}
	}, TLSHandshakeStart: func() { tlsStart = time.Now() }, TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
		if !tlsStart.IsZero() {
			r.tls += time.Since(tlsStart)
		}
	}, GotConn: func(info httptrace.GotConnInfo) { r.reused = info.Reused }, GotFirstResponseByte: func() { firstByte = time.Now(); r.ttfb = firstByte.Sub(requestStart) }}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, rawURL, nil)
	if err != nil {
		r.err = err
		return r
	}
	req.Header.Set("User-Agent", "wowdata-network-calibration/1")
	req.Header.Set("Cache-Control", "no-cache")
	if start >= 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	}
	requestStart = time.Now()
	resp, err := client.Do(req)
	if err != nil {
		r.err = err
		return r
	}
	defer resp.Body.Close()
	r.status, r.proto, r.contentRange = resp.Status, resp.Proto, resp.Header.Get("Content-Range")
	bodyStart := firstByte
	if bodyStart.IsZero() {
		bodyStart = time.Now()
	}
	h := sha256.New()
	r.bytes, err = io.Copy(h, resp.Body)
	r.body = time.Since(bodyStart)
	r.hash = hex.EncodeToString(h.Sum(nil))
	if err != nil {
		r.err = err
	} else if start >= 0 && resp.StatusCode != http.StatusPartialContent {
		r.err = fmt.Errorf("range request returned HTTP %d", resp.StatusCode)
	} else if start < 0 && resp.StatusCode != http.StatusOK {
		r.err = fmt.Errorf("metadata request returned HTTP %d", resp.StatusCode)
	}
	return r
}

func summarize(samples []sample) []sourceSummary {
	groups := map[string][]sample{}
	var names []string
	for _, s := range samples {
		if _, ok := groups[s.Name]; !ok {
			names = append(names, s.Name)
		}
		groups[s.Name] = append(groups[s.Name], s)
	}
	var out []sourceSummary
	for _, name := range names {
		x := sourceSummary{Name: name, Protocols: map[string]int{}}
		var walls, ttfbs, dns, connects, handshakes []int64
		var rates []float64
		for _, s := range groups[name] {
			if s.Status != "pass" {
				x.Failed++
				continue
			}
			x.Successful++
			walls = append(walls, s.WallNanos)
			ttfbs = append(ttfbs, s.TTFBNanos)
			dns = append(dns, s.DNSNanos)
			connects = append(connects, s.ConnectNanos)
			handshakes = append(handshakes, s.TLSNanos)
			rates = append(rates, s.BytesPerSecond)
			for p, n := range s.Protocols {
				x.Protocols[p] += n
			}
		}
		sort.Slice(walls, func(i, j int) bool { return walls[i] < walls[j] })
		sort.Slice(ttfbs, func(i, j int) bool { return ttfbs[i] < ttfbs[j] })
		sort.Slice(dns, func(i, j int) bool { return dns[i] < dns[j] })
		sort.Slice(connects, func(i, j int) bool { return connects[i] < connects[j] })
		sort.Slice(handshakes, func(i, j int) bool { return handshakes[i] < handshakes[j] })
		sort.Float64s(rates)
		x.P50WallNanos = pct64(walls, .5)
		x.P95WallNanos = pct64(walls, .95)
		x.P50TTFBNanos = pct64(ttfbs, .5)
		x.P95TTFBNanos = pct64(ttfbs, .95)
		x.P50DNSNanos = pct64(dns, .5)
		x.P50ConnectNanos = pct64(connects, .5)
		x.P50TLSNanos = pct64(handshakes, .5)
		x.P50BytesPerSecond = pctFloat(rates, .5)
		x.P95BytesPerSecond = pctFloat(rates, .95)
		out = append(out, x)
	}
	return out
}

func loadTournament(path string) (tournamentReport, error) {
	var r tournamentReport
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &r)
	}
	return r, err
}
func decide(metadata, largeRange, chunk tournamentReport, sources []sourceSummary, cfg config) selection {
	md := selectStableCandidate(metadata, "workers", cfg.MetadataTournament)
	lr := selectStableCandidate(largeRange, "workers", cfg.LargeRangeTournament)
	ch := selectStableCandidate(chunk, "MiB", cfg.ChunkTournament)
	var single, multi, meta sourceSummary
	for _, s := range sources {
		switch s.Name {
		case "cdn-single":
			single = s
		case "cdn-multi":
			multi = s
		case "cdn-metadata":
			meta = s
		}
	}
	speedup := 0.0
	if single.P50BytesPerSecond > 0 {
		speedup = multi.P50BytesPerSecond / single.P50BytesPerSecond
	}
	recommended := 1
	if speedup > 1.15 {
		recommended = cfg.MultiConnections
	}
	if recommended > 4 {
		recommended = 4
	}
	network := networkDecision{SingleBps: single.P50BytesPerSecond, MultiBps: multi.P50BytesPerSecond, MultiSpeedup: speedup, RTTP50Ms: float64(meta.P50ConnectNanos) / 1e6, RTTEstimateMethod: "TCP connect duration (approximately one network RTT; excludes DNS, TLS and server TTFB)", BDPBytes: single.P50BytesPerSecond * (float64(meta.P50ConnectNanos) / 1e9), RecommendedConnections: recommended, Rule: "same-round p50 throughput and TCP-connect RTT estimate BDP; clamp by global connection, memory, and error budgets"}
	return selection{Metadata: md, LargeRange: lr, Chunk: ch, Network: network}
}

func selectStableCandidate(tournament tournamentReport, unit, source string) candidateDecision {
	decision := candidateDecision{
		Unit: unit, Source: filepath.ToSlash(source), Candidates: tournament.Summary,
		Rule: "among zero-failure candidates within 10% of the fastest p50, choose the lowest p95; break ties by p50, peak working set, then candidate value",
	}
	bestP50 := 1e308
	for _, candidate := range tournament.Summary {
		if candidate.SuccessfulRuns > 0 && candidate.P50WallMs > 0 && candidate.P50WallMs < bestP50 {
			bestP50 = candidate.P50WallMs
		}
	}
	eligible := make([]tournamentSummary, 0, len(tournament.Summary))
	for _, candidate := range tournament.Summary {
		if candidate.SuccessfulRuns > 0 && candidate.FailedAttempts == 0 && candidate.P50WallMs <= bestP50*1.10 {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) == 0 {
		for _, candidate := range tournament.Summary {
			if candidate.SuccessfulRuns > 0 && candidate.P50WallMs <= bestP50*1.10 {
				eligible = append(eligible, candidate)
			}
		}
	}
	sort.Slice(eligible, func(i, j int) bool {
		left, right := eligible[i], eligible[j]
		if left.P95WallMs != right.P95WallMs {
			return left.P95WallMs < right.P95WallMs
		}
		if left.P50WallMs != right.P50WallMs {
			return left.P50WallMs < right.P50WallMs
		}
		if left.MaxPeakWorkingSetBytes != right.MaxPeakWorkingSetBytes {
			return left.MaxPeakWorkingSetBytes < right.MaxPeakWorkingSetBytes
		}
		return left.Candidate < right.Candidate
	})
	if len(eligible) > 0 {
		decision.Initial = eligible[0].Candidate
		decision.Retained = []int{decision.Initial}
	}
	for _, candidate := range tournament.Summary {
		if candidate.Candidate == decision.Initial {
			continue
		}
		decision.Eliminated = append(decision.Eliminated, eliminated{candidate.Candidate, fmt.Sprintf("p50 %.1fms, p95 %.1fms, failed attempts %d, peak working set %.0f bytes", candidate.P50WallMs, candidate.P95WallMs, candidate.FailedAttempts, candidate.MaxPeakWorkingSetBytes)})
	}
	return decision
}
func pct64(v []int64, q float64) int64 {
	if len(v) == 0 {
		return 0
	}
	return v[int(float64(len(v)-1)*q+.5)]
}
func pctFloat(v []float64, q float64) float64 {
	if len(v) == 0 {
		return 0
	}
	return v[int(float64(len(v)-1)*q+.5)]
}
func max64(a, b int64) int64 {
	if b > a {
		return b
	}
	return a
}
