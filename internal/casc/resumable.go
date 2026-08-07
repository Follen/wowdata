package casc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wowdata/internal/resource"
	"wowdata/internal/storage"
)

var errRangeIgnored = errors.New("server ignored byte range")

const DefaultResumeTTL = 24 * time.Hour

const (
	defaultSmallObjectDirectThreshold = 512 << 10
	defaultResumeCheckpointChunks     = 8
	defaultResumeCheckpointPeriod     = 250 * time.Millisecond
)

type ResumeOptions struct {
	Workers        int
	CacheRoot      string
	MaxBytes       int64
	ObjectMaxBytes int64
	TTL            time.Duration
	Budget         *CacheBudget
	Scheduler      *resource.Scheduler
	ChunkSize      int64
	Context        context.Context
	KeepPart       bool
	Adaptive       bool
	MergeRanges    bool
	// RangeURLs contains equivalent immutable object URLs for benchmark probes.
	// The primary URL remains the identity and HEAD source.
	RangeURLs []string
}

type cacheQuotaState struct {
	Schema         string `json:"schema"`
	AccountedBytes int64  `json:"accountedBytes"`
}

// CacheBudget scans once per process and then coordinates O(1) reservations
// through a compact cross-process counter protected by a filesystem lock.
type CacheBudget struct {
	mu          sync.Mutex
	root        string
	maxBytes    int64
	initialized bool
	accounted   int64
}

func NewCacheBudget(root string, maxBytes int64) *CacheBudget {
	return &CacheBudget{root: root, maxBytes: maxBytes}
}

type CacheQuotaError struct {
	CurrentBytes   int64
	ReservedBytes  int64
	ProjectedBytes int64
	MaxBytes       int64
}

func (e *CacheQuotaError) Error() string {
	return fmt.Sprintf("cache quota exceeded before download: current=%d reserve=%d projected=%d max=%d", e.CurrentBytes, e.ReservedBytes, e.ProjectedBytes, e.MaxBytes)
}

func IsCacheQuotaError(err error) bool {
	var quotaErr *CacheQuotaError
	return errors.As(err, &quotaErr)
}

type resumeState struct {
	URL       string   `json:"url"`
	Size      int64    `json:"size"`
	ChunkSize int64    `json:"chunkSize"`
	ETag      string   `json:"etag,omitempty"`
	Modified  string   `json:"lastModified,omitempty"`
	Complete  []bool   `json:"complete"`
	Hashes    []string `json:"hashes,omitempty"`
}

type resumeRangeJob struct {
	start   int64
	end     int64
	indexes []int
}

func buildResumeJobs(state resumeState, size, chunkSize, mergeLimit int64) []resumeRangeJob {
	jobs := make([]resumeRangeJob, 0)
	for index := 0; index < len(state.Complete); {
		if state.Complete[index] {
			index++
			continue
		}
		startIndex, endIndex := index, index
		start := int64(index) * chunkSize
		end := start + chunkSize
		if end > size {
			end = size
		}
		for endIndex+1 < len(state.Complete) && !state.Complete[endIndex+1] {
			nextEnd := int64(endIndex+2) * chunkSize
			if nextEnd > size {
				nextEnd = size
			}
			if mergeLimit <= 0 || nextEnd-start > mergeLimit {
				break
			}
			endIndex++
			end = nextEnd
		}
		indexes := make([]int, 0, endIndex-startIndex+1)
		for i := startIndex; i <= endIndex; i++ {
			indexes = append(indexes, i)
		}
		jobs = append(jobs, resumeRangeJob{start: start, end: end - 1, indexes: indexes})
		index = endIndex + 1
	}
	return jobs
}

// DownloadHTTPConcurrentResumable downloads a range-capable object into an
// in-place .part file and returns the verified bytes after publication.
// State is deliberately compact and contains no decoded/parsed payload.
func DownloadHTTPConcurrentResumable(url, partPath, statePath string, workers int) ([]byte, error) {
	return DownloadHTTPConcurrentResumableWithOptions(url, partPath, statePath, ResumeOptions{Workers: workers, TTL: DefaultResumeTTL})
}

// DownloadHTTPFullObjectWithContext performs a bounded full-object probe.
func DownloadHTTPFullObjectWithContext(url string, ctx context.Context) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	head, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(head)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength <= 0 {
		return nil, fmt.Errorf("HTTP %d head from %s", resp.StatusCode, url)
	}
	return downloadHTTPFull(ctx, url, resp.ContentLength)
}

func DownloadHTTPConcurrentResumableWithOptions(url, partPath, statePath string, opts ResumeOptions) ([]byte, error) {
	started := time.Now()
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = rangeWorkerCount
	}
	state := loadResumeState(statePath)
	if opts.TTL > 0 && resumeStateExpired(statePath, opts.TTL) {
		_ = os.Remove(partPath)
		_ = os.Remove(statePath)
		state = resumeState{}
	}
	identityComplete := state.URL == url && state.Size > 0 && state.ChunkSize > 0 && len(state.Complete) > 0 && (state.ETag != "" || state.Modified != "")
	var size int64
	var acceptRanges bool
	var etag, modified string
	if identityComplete {
		size, acceptRanges = state.Size, true
		etag, modified = state.ETag, state.Modified
	} else {
		head, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := httpClient.Do(head)
		if err != nil {
			return nil, err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d head from %s", resp.StatusCode, url)
		}
		if resp.ContentLength <= 0 {
			return nil, fmt.Errorf("remote object has no content length")
		}
		size = resp.ContentLength
		acceptRanges = supportsByteRanges(resp.Header.Get("Accept-Ranges"))
		etag = resp.Header.Get("ETag")
		modified = resp.Header.Get("Last-Modified")
	}
	if opts.ObjectMaxBytes > 0 && size > opts.ObjectMaxBytes {
		return nil, fmt.Errorf("remote object exceeds object byte limit: size=%d max=%d", size, opts.ObjectMaxBytes)
	}
	budget := opts.Budget
	if budget == nil && opts.CacheRoot != "" && opts.MaxBytes > 0 {
		budget = NewCacheBudget(opts.CacheRoot, opts.MaxBytes)
	}
	if err := reserveResumeSpace(partPath, size, budget); err != nil {
		return nil, err
	}
	if !acceptRanges || (opts.Adaptive && size <= defaultSmallObjectDirectThreshold) {
		_ = os.Remove(statePath)
		data, err := downloadHTTPFull(ctx, url, size)
		return finishFullFallback(partPath, data, opts.KeepPart, err)
	}
	chunkSize := opts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = adaptiveResumeChunkSize(size)
	}
	chunkCount := int((size + chunkSize - 1) / chunkSize)
	if state.URL != url || state.Size != size || state.ChunkSize != chunkSize || len(state.Complete) != chunkCount || state.ETag != etag || state.Modified != modified {
		state = resumeState{URL: url, Size: size, ChunkSize: chunkSize, ETag: etag, Modified: modified, Complete: make([]bool, chunkCount), Hashes: make([]string, chunkCount)}
	} else if len(state.Hashes) != chunkCount {
		state.Hashes = make([]string, chunkCount)
	}
	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := file.Truncate(size); err != nil {
		return nil, err
	}
	if err := validateCompletedResumeChunks(file, &state); err != nil {
		return nil, err
	}
	if allResumeChunksComplete(state.Complete) {
		if err := file.Sync(); err != nil {
			return nil, err
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
		if opts.KeepPart {
			_ = os.Remove(statePath)
			return nil, nil
		}
		data, err := resource.ReadFile(partPath)
		if err != nil {
			return nil, err
		}
		if int64(len(data)) != size {
			return nil, fmt.Errorf("resumed object length mismatch: got %d want %d", len(data), size)
		}
		_ = os.Remove(partPath)
		_ = os.Remove(statePath)
		return data, nil
	}
	reusedChunks := 0
	reusedBytes := int64(0)
	for index, complete := range state.Complete {
		if complete {
			reusedChunks++
			reusedBytes += resumeChunkLength(index, chunkSize, size)
		}
	}
	if err := saveResumeState(statePath, state); err != nil {
		return nil, err
	}
	var stateMu sync.Mutex
	var firstErr error
	var rangeIgnored bool
	var rangeLengthMismatch bool
	downloadedChunks := 0
	downloadedBytes := int64(0)
	dirtyChunks := 0
	lastCheckpoint := time.Now()
	mergeLimit := int64(0)
	if opts.MergeRanges {
		mergeLimit = chunkSize * 4
	}
	jobs := make(chan resumeRangeJob)
	var wg sync.WaitGroup
	if workers > chunkCount {
		workers = chunkCount
	}
	if opts.Workers <= 0 && size >= 128<<20 && workers < 8 {
		workers *= 2
		if workers > 8 {
			workers = 8
		}
	}
	if workers < 1 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				stateMu.Lock()
				stop := rangeIgnored
				stateMu.Unlock()
				if stop {
					continue
				}
				data, fetchErr := fetchResumeRange(ctx, url, job.start, job.end, opts.Scheduler, opts.RangeURLs)
				if fetchErr != nil {
					stateMu.Lock()
					if errors.Is(fetchErr, errRangeIgnored) {
						rangeIgnored = true
					}
					if firstErr == nil {
						firstErr = fetchErr
					}
					stateMu.Unlock()
					continue
				}
				if int64(len(data)) != job.end-job.start+1 {
					stateMu.Lock()
					rangeLengthMismatch = true
					if firstErr == nil {
						firstErr = fmt.Errorf("range %d-%d returned %d bytes", job.start, job.end, len(data))
					}
					stateMu.Unlock()
					continue
				}
				writeErr := error(nil)
				for _, index := range job.indexes {
					start := int64(index) * chunkSize
					end := start + chunkSize
					if end > size {
						end = size
					}
					offset := start - job.start
					if _, err := file.WriteAt(data[offset:offset+end-start], start); err != nil {
						writeErr = err
						break
					}
				}
				if writeErr != nil {
					stateMu.Lock()
					if firstErr == nil {
						firstErr = writeErr
					}
					stateMu.Unlock()
					continue
				}
				stateMu.Lock()
				for _, index := range job.indexes {
					start := int64(index) * chunkSize
					end := start + chunkSize
					if end > size {
						end = size
					}
					offset := start - job.start
					state.Complete[index] = true
					state.Hashes[index] = hashBytes(data[offset : offset+end-start])
					downloadedChunks++
					dirtyChunks++
				}
				downloadedBytes += int64(len(data))
				if downloadedChunks == 1 || dirtyChunks >= defaultResumeCheckpointChunks || time.Since(lastCheckpoint) >= defaultResumeCheckpointPeriod {
					if checkpointErr := saveResumeState(statePath, state); checkpointErr != nil && firstErr == nil {
						firstErr = checkpointErr
					} else if checkpointErr == nil {
						dirtyChunks = 0
						lastCheckpoint = time.Now()
					}
				}
				stateMu.Unlock()
			}
		}()
	}
	queueCanceled := false
	for _, job := range buildResumeJobs(state, size, chunkSize, mergeLimit) {
		select {
		case jobs <- job:
		case <-ctx.Done():
			stateMu.Lock()
			if firstErr == nil {
				firstErr = ctx.Err()
			}
			stateMu.Unlock()
			queueCanceled = true
		}
		if queueCanceled {
			break
		}
	}
	close(jobs)
	wg.Wait()
	stateMu.Lock()
	if dirtyChunks > 0 {
		if checkpointErr := saveResumeState(statePath, state); checkpointErr != nil && firstErr == nil {
			firstErr = checkpointErr
		}
	}
	stateMu.Unlock()
	if rangeIgnored || rangeLengthMismatch {
		if err := file.Close(); err != nil {
			return nil, err
		}
		_ = os.Remove(statePath)
		data, err := downloadHTTPFull(ctx, url, size)
		return finishFullFallback(partPath, data, opts.KeepPart, err)
	}
	if firstErr != nil {
		return nil, firstErr
	}
	for _, done := range state.Complete {
		if !done {
			return nil, fmt.Errorf("resume state incomplete")
		}
	}
	if err := file.Sync(); err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if opts.KeepPart {
		if downloadProgressWriter != nil {
			fmt.Fprintf(downloadProgressWriter, "download method=range-resume url=%s bytes=%d reusedBytes=%d downloadedBytes=%d reusedChunks=%d downloadedChunks=%d workers=%d duration=%s\n",
				url, size, reusedBytes, downloadedBytes, reusedChunks, downloadedChunks, workers, time.Since(started).Round(time.Millisecond))
		}
		_ = os.Remove(statePath)
		return nil, nil
	}
	data, err := resource.ReadFile(partPath)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, fmt.Errorf("resumed object length mismatch: got %d want %d", len(data), size)
	}
	if downloadProgressWriter != nil {
		fmt.Fprintf(downloadProgressWriter, "download method=range-resume url=%s bytes=%d reusedBytes=%d downloadedBytes=%d reusedChunks=%d downloadedChunks=%d workers=%d duration=%s\n",
			url, size, reusedBytes, downloadedBytes, reusedChunks, downloadedChunks, workers, time.Since(started).Round(time.Millisecond))
	}
	_ = os.Remove(partPath)
	_ = os.Remove(statePath)
	return data, nil
}

func finishFullFallback(partPath string, data []byte, keepPart bool, downloadErr error) ([]byte, error) {
	if downloadErr != nil {
		return nil, downloadErr
	}
	if keepPart {
		if err := storage.AtomicWriteFile(partPath, data, 0o644); err != nil {
			return nil, err
		}
		return nil, nil
	}
	_ = os.Remove(partPath)
	return data, nil
}

func resumeChunkLength(index int, chunkSize, size int64) int64 {
	start := int64(index) * chunkSize
	if remaining := size - start; remaining < chunkSize {
		return remaining
	}
	return chunkSize
}

// adaptiveResumeChunkSize selects a range size that preserves the historical
// 1 MiB behavior for small objects while reducing request overhead for large
// CDN objects. Callers can still override this through ResumeOptions.ChunkSize.
func adaptiveResumeChunkSize(size int64) int64 {
	if size >= 128<<20 {
		return 8 << 20
	}
	if size >= 16<<20 {
		return 4 << 20
	}
	return int64(rangeChunkSize)
}

func allResumeChunksComplete(complete []bool) bool {
	if len(complete) == 0 {
		return false
	}
	for _, done := range complete {
		if !done {
			return false
		}
	}
	return true
}

func reserveResumeSpace(partPath string, size int64, budget *CacheBudget) error {
	if err := os.MkdirAll(filepath.Dir(partPath), 0755); err != nil {
		return err
	}
	if budget != nil && budget.root != "" && budget.maxBytes > 0 {
		budget.mu.Lock()
		defer budget.mu.Unlock()
		lock, err := storage.AcquirePathLock(filepath.Join(budget.root, "casc", "quota_locks"), "cache-budget", 30*time.Second)
		if err != nil {
			return err
		}
		defer lock.Release()
		statePath := filepath.Join(budget.root, "casc", "quota.json")
		state := cacheQuotaState{}
		if data, readErr := resource.ReadFile(statePath); readErr == nil {
			_ = json.Unmarshal(data, &state)
		}
		if !budget.initialized {
			actual, sizeErr := storage.DirSize(budget.root)
			if sizeErr != nil {
				return sizeErr
			}
			budget.accounted = actual + cacheMetadataHeadroom(budget.maxBytes)
			budget.initialized = true
		}
		if state.Schema == "wowdata.cache-quota.v1" && state.AccountedBytes > budget.accounted {
			budget.accounted = state.AccountedBytes
		}
		existing := int64(0)
		if info, statErr := os.Stat(partPath); statErr == nil {
			existing = info.Size()
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		projected := budget.accounted - existing + size
		if projected > budget.maxBytes {
			return &CacheQuotaError{CurrentBytes: budget.accounted, ReservedBytes: size - existing, ProjectedBytes: projected, MaxBytes: budget.maxBytes}
		}
		file, openErr := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0644)
		if openErr != nil {
			return openErr
		}
		truncateErr := file.Truncate(size)
		closeErr := file.Close()
		if truncateErr != nil {
			return truncateErr
		}
		if closeErr != nil {
			return closeErr
		}
		state = cacheQuotaState{Schema: "wowdata.cache-quota.v1", AccountedBytes: projected}
		if err := storage.AtomicWriteJSON(statePath, state, 0644); err != nil {
			budget.initialized = false
			return err
		}
		budget.accounted = projected
		return nil
	}
	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Truncate(size)
}

func cacheMetadataHeadroom(maxBytes int64) int64 {
	headroom := maxBytes / 100
	if headroom > 1<<20 {
		headroom = 1 << 20
	}
	if headroom < 4096 {
		headroom = 4096
	}
	return headroom
}

func resumeStateExpired(path string, ttl time.Duration) bool {
	info, err := os.Stat(path)
	return err == nil && time.Since(info.ModTime()) >= ttl
}

func supportsByteRanges(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "bytes")
}

func downloadHTTPFull(ctx context.Context, url string, expectedSize int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d get from %s", resp.StatusCode, url)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != expectedSize {
		return nil, fmt.Errorf("full object length mismatch: got %d want %d", len(data), expectedSize)
	}
	return data, nil
}

func validateCompletedResumeChunks(file *os.File, state *resumeState) error {
	for index, complete := range state.Complete {
		if !complete {
			continue
		}
		start := int64(index) * state.ChunkSize
		length := state.ChunkSize
		if remaining := state.Size - start; remaining < length {
			length = remaining
		}
		if length <= 0 || state.Hashes[index] == "" {
			state.Complete[index] = false
			state.Hashes[index] = ""
			continue
		}
		data := make([]byte, length)
		if _, err := file.ReadAt(data, start); err != nil && err != io.EOF {
			return err
		}
		if hashBytes(data) != state.Hashes[index] {
			state.Complete[index] = false
			state.Hashes[index] = ""
		}
	}
	return nil
}

func hashBytes(data []byte) string {
	sum := resource.SumSHA256(data)
	return hex.EncodeToString(sum[:])
}

func fetchResumeRange(ctx context.Context, url string, start, end int64, scheduler *resource.Scheduler, rangeURLs []string) ([]byte, error) {
	var lastErr error
	urls := make([]string, 0, len(rangeURLs)+1)
	seen := make(map[string]struct{}, len(rangeURLs)+1)
	for _, candidate := range append([]string{url}, rangeURLs...) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		urls = append(urls, candidate)
	}
	if len(urls) == 0 {
		urls = []string{url}
	}
	for attempt := 0; attempt < httpDownloadAttempts; attempt++ {
		release, acquireErr := scheduler.Acquire(ctx, resource.LargeRangePool)
		if acquireErr != nil {
			return nil, acquireErr
		}
		candidate := urls[(int(start/int64(rangeChunkSize))+attempt)%len(urls)]
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate, nil)
		if err != nil {
			release()
			return nil, err
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		resp, err := httpClient.Do(req)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				release()
				return nil, errRangeIgnored
			}
			data, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			release()
			if readErr == nil && resp.StatusCode == http.StatusPartialContent {
				return data, nil
			}
			if readErr != nil {
				lastErr = readErr
			} else {
				lastErr = fmt.Errorf("HTTP %d for range %d-%d", resp.StatusCode, start, end)
			}
		} else {
			release()
			lastErr = err
		}
		if attempt+1 < httpDownloadAttempts {
			timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			}
		}
	}
	return nil, lastErr
}

func loadResumeState(path string) resumeState {
	data, err := resource.ReadFile(path)
	if err != nil {
		return resumeState{}
	}
	var state resumeState
	if json.Unmarshal(data, &state) != nil {
		return resumeState{}
	}
	return state
}

func saveResumeState(path string, state resumeState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return storage.AtomicWriteFile(path, data, 0644)
}

func ResumeObjectPaths(root, url string) (string, string) {
	sum := resource.SumSHA256([]byte(url))
	name := hex.EncodeToString(sum[:])
	return filepath.Join(root, name+".part"), filepath.Join(root, name+".json")
}
