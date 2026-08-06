package casc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"wowdata/internal/resource"
)

type HTTPMetrics struct {
	Requests           int64            `json:"requests"`
	RangeRequests      int64            `json:"rangeRequests"`
	FailedRequests     int64            `json:"failedRequests"`
	CanceledRequests   int64            `json:"canceledRequests"`
	Connections        int64            `json:"connections"`
	ReusedConnections  int64            `json:"reusedConnections"`
	ResponseBytes      int64            `json:"responseBytes"`
	UniquePayloadBytes int64            `json:"uniquePayloadBytes"`
	DuplicateBytes     int64            `json:"duplicatePayloadBytes"`
	RequestTimeNanos   int64            `json:"requestTimeNanos"`
	Protocols          map[string]int64 `json:"protocols"`
	DuplicateByURL     map[string]int64 `json:"duplicateByUrl,omitempty"`
	StageUniqueBytes   int64            `json:"-"`
	StageTransferBytes int64            `json:"-"`
}

type byteInterval struct {
	start int64
	end   int64
}

type httpMetricState struct {
	mu        sync.Mutex
	value     HTTPMetrics
	intervals map[string][]byteInterval
}

var globalHTTPMetrics = newHTTPMetricState()

func newHTTPMetricState() *httpMetricState {
	return &httpMetricState{value: HTTPMetrics{Protocols: make(map[string]int64), DuplicateByURL: make(map[string]int64)}, intervals: make(map[string][]byteInterval)}
}

func ResetHTTPMetrics() {
	globalHTTPMetrics.mu.Lock()
	globalHTTPMetrics.value = HTTPMetrics{Protocols: make(map[string]int64), DuplicateByURL: make(map[string]int64)}
	globalHTTPMetrics.intervals = make(map[string][]byteInterval)
	globalHTTPMetrics.mu.Unlock()
}

func SnapshotHTTPMetrics() HTTPMetrics {
	globalHTTPMetrics.mu.Lock()
	defer globalHTTPMetrics.mu.Unlock()
	result := globalHTTPMetrics.value
	result.Protocols = make(map[string]int64, len(globalHTTPMetrics.value.Protocols))
	for protocol, count := range globalHTTPMetrics.value.Protocols {
		result.Protocols[protocol] = count
	}
	result.DuplicateByURL = make(map[string]int64, len(globalHTTPMetrics.value.DuplicateByURL))
	for url, count := range globalHTTPMetrics.value.DuplicateByURL {
		result.DuplicateByURL[url] = count
	}
	return result
}

type instrumentedTransport struct {
	base http.RoundTripper
}

// InstrumentHTTPClient returns a shallow copy of client whose requests are
// included in the process-wide CASC HTTP metrics snapshot.
func InstrumentHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	result := *client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if _, ok := base.(instrumentedTransport); !ok {
		result.Transport = instrumentedTransport{base: base}
	}
	return &result
}

// InstrumentHTTPClientForStage binds every request made by the returned client
// to one stage through the request context, preserving concurrent attribution.
func InstrumentHTTPClientForStage(client *http.Client, stage resource.StageID) *http.Client {
	result := InstrumentHTTPClient(client)
	result.Transport = stageTransport{base: result.Transport, stage: stage}
	return result
}

type stageTransport struct {
	base  http.RoundTripper
	stage resource.StageID
}

func (t stageTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(resource.ContextWithStage(request.Context(), t.stage))
	return t.base.RoundTrip(request)
}

func (t instrumentedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	started := time.Now()
	trace := &httptrace.ClientTrace{
		ConnectStart: func(_, _ string) {
			globalHTTPMetrics.mu.Lock()
			globalHTTPMetrics.value.Connections++
			globalHTTPMetrics.mu.Unlock()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			if info.Reused {
				globalHTTPMetrics.mu.Lock()
				globalHTTPMetrics.value.ReusedConnections++
				globalHTTPMetrics.mu.Unlock()
			}
		},
	}
	request = request.Clone(httptrace.WithClientTrace(request.Context(), trace))
	globalHTTPMetrics.mu.Lock()
	globalHTTPMetrics.value.Requests++
	if request.Header.Get("Range") != "" {
		globalHTTPMetrics.value.RangeRequests++
	}
	globalHTTPMetrics.mu.Unlock()
	response, err := t.base.RoundTrip(request)
	duration := time.Since(started).Nanoseconds()
	if duration < 1 {
		// Windows' monotonic clock can report zero for an in-process test server.
		// Keep the aggregate usable as a strictly positive timing counter.
		duration = 1
	}
	globalHTTPMetrics.mu.Lock()
	globalHTTPMetrics.value.RequestTimeNanos += duration
	if err != nil {
		if errors.Is(err, context.Canceled) {
			globalHTTPMetrics.value.CanceledRequests++
		} else {
			globalHTTPMetrics.value.FailedRequests++
		}
		globalHTTPMetrics.mu.Unlock()
		return nil, err
	}
	if response.StatusCode >= 400 {
		globalHTTPMetrics.value.FailedRequests++
	}
	globalHTTPMetrics.value.Protocols[response.Proto]++
	globalHTTPMetrics.mu.Unlock()
	response.Body = &metricBody{ReadCloser: response.Body, url: request.URL.String(), interval: responseInterval(request, response), stage: resource.StageFromContext(request.Context())}
	return response, nil
}

type metricBody struct {
	io.ReadCloser
	url      string
	interval byteInterval
	bytes    int64
	once     sync.Once
	stage    resource.StageID
}

func (b *metricBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	b.bytes += int64(n)
	if err == io.EOF {
		b.record()
	}
	return n, err
}

func (b *metricBody) Close() error {
	b.record()
	return b.ReadCloser.Close()
}

func (b *metricBody) record() {
	b.once.Do(func() {
		globalHTTPMetrics.mu.Lock()
		defer globalHTTPMetrics.mu.Unlock()
		globalHTTPMetrics.value.ResponseBytes += b.bytes
		if b.bytes <= 0 {
			return
		}
		if b.interval.end <= b.interval.start {
			b.interval = byteInterval{start: 0, end: b.bytes}
		}
		end := b.interval.start + b.bytes
		if end > b.interval.end {
			end = b.interval.end
		}
		added := addHTTPInterval(globalHTTPMetrics.intervals, b.url, byteInterval{start: b.interval.start, end: end})
		globalHTTPMetrics.value.UniquePayloadBytes += added
		duplicate := b.bytes - added
		globalHTTPMetrics.value.DuplicateBytes += duplicate
		if duplicate > 0 {
			globalHTTPMetrics.value.DuplicateByURL[b.url] += duplicate
		}
		if b.stage != 0 {
			globalHTTPMetrics.value.StageUniqueBytes += added
			globalHTTPMetrics.value.StageTransferBytes += b.bytes
			resource.RecordStageNetwork(b.stage, uint64(added), uint64(b.bytes))
		}
	})
}

func responseInterval(request *http.Request, response *http.Response) byteInterval {
	if value := response.Header.Get("Content-Range"); value != "" {
		var start, end int64
		if _, err := fmt.Sscanf(value, "bytes %d-%d/", &start, &end); err == nil && end >= start {
			return byteInterval{start: start, end: end + 1}
		}
	}
	if request.Method == http.MethodGet && response.ContentLength > 0 {
		return byteInterval{start: 0, end: response.ContentLength}
	}
	return byteInterval{}
}

func addHTTPInterval(intervals map[string][]byteInterval, url string, next byteInterval) int64 {
	existing := intervals[url]
	var before int64
	for _, item := range existing {
		before += item.end - item.start
	}
	current := make([]byteInterval, 0, len(existing)+1)
	current = append(current, existing...)
	current = append(current, next)
	sort.Slice(current, func(i, j int) bool {
		if current[i].start == current[j].start {
			return current[i].end < current[j].end
		}
		return current[i].start < current[j].start
	})
	merged := current[:0]
	for _, item := range current {
		if len(merged) == 0 || item.start > merged[len(merged)-1].end {
			merged = append(merged, item)
			continue
		}
		if item.end > merged[len(merged)-1].end {
			merged[len(merged)-1].end = item.end
		}
	}
	var after int64
	for _, item := range merged {
		after += item.end - item.start
	}
	intervals[url] = merged
	return after - before
}

func ParseRangeLength(value string) int64 {
	value = strings.TrimSpace(strings.TrimPrefix(value, "bytes="))
	if strings.HasPrefix(value, "-") {
		length, _ := strconv.ParseInt(strings.TrimPrefix(value, "-"), 10, 64)
		return length
	}
	parts := strings.SplitN(value, "-", 2)
	if len(parts) != 2 {
		return 0
	}
	start, startErr := strconv.ParseInt(parts[0], 10, 64)
	end, endErr := strconv.ParseInt(parts[1], 10, 64)
	if startErr != nil || endErr != nil || end < start {
		return 0
	}
	return end - start + 1
}
