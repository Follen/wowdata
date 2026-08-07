package hotfix

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

const WagoParserVersion = "wago-inertia-v1"

type WagoPageMeta struct {
	CurrentPage    int       `json:"current_page"`
	LastPage       int       `json:"last_page"`
	PerPage        int       `json:"per_page"`
	Total          int64     `json:"total"`
	NextPageURL    string    `json:"next_page_url,omitempty"`
	ResponseSHA256 string    `json:"response_sha256"`
	EmbeddedJSON   []byte    `json:"-"`
	CapturedAt     time.Time `json:"captured_at"`
	CacheHit       bool      `json:"cache_hit"`
	RequestKey     string    `json:"request_key,omitempty"`
}

type flexInt64 int64

func (v *flexInt64) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		*v = 0
		return nil
	}
	var n json.Number
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		n = json.Number(s)
	} else {
		n = json.Number(string(b))
	}
	x, err := strconv.ParseInt(string(n), 10, 64)
	if err != nil {
		return err
	}
	*v = flexInt64(x)
	return nil
}

type wagoRecord struct {
	ID         flexInt64    `json:"id"`
	PushID     flexInt64    `json:"push_id"`
	RecordID   flexInt64    `json:"record_id"`
	Status     flexInt64    `json:"status"`
	Build      stringNumber `json:"build"`
	TableName  string       `json:"table_name"`
	Data       any          `json:"data"`
	CreatedAt  string       `json:"created_at"`
	RegionID   flexInt64    `json:"region_id"`
	Locale     string       `json:"locale"`
	SearchText string       `json:"search_text"`
}
type stringNumber string

func (s *stringNumber) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = stringNumber(v)
		return nil
	}
	*s = stringNumber(string(b))
	return nil
}

type wagoPagination struct {
	CurrentPage int          `json:"current_page"`
	LastPage    int          `json:"last_page"`
	PerPage     int          `json:"per_page"`
	Total       flexInt64    `json:"total"`
	Data        []wagoRecord `json:"data"`
	NextPageURL string       `json:"next_page_url"`
}

func ParseWagoHTML(raw []byte, q Query) (Result, WagoPageMeta, error) {
	if err := q.Validate(); err != nil {
		return Result{}, WagoPageMeta{}, err
	}
	embedded, err := findInertiaPage(raw)
	if err != nil {
		return Result{}, WagoPageMeta{}, err
	}
	var root map[string]json.RawMessage
	if err = json.Unmarshal(embedded, &root); err != nil {
		return Result{}, WagoPageMeta{}, errf("hotfix_protocol_changed", "data-page", "invalid JSON: %v", err)
	}
	var component string
	if v, ok := root["component"]; !ok || json.Unmarshal(v, &component) != nil || component != "Hotfixes" {
		return Result{}, WagoPageMeta{}, errf("hotfix_protocol_changed", "component", "expected Hotfixes")
	}
	var props map[string]json.RawMessage
	if v, ok := root["props"]; !ok || json.Unmarshal(v, &props) != nil {
		return Result{}, WagoPageMeta{}, errf("hotfix_protocol_changed", "props", "missing object")
	}
	v, ok := props["hotfixes"]
	if !ok {
		return Result{}, WagoPageMeta{}, errf("hotfix_protocol_changed", "props.hotfixes", "missing pagination object")
	}
	var page wagoPagination
	if err = json.Unmarshal(v, &page); err != nil {
		return Result{}, WagoPageMeta{}, errf("hotfix_protocol_changed", "props.hotfixes", "%v", err)
	}
	if page.CurrentPage < 1 || page.LastPage < page.CurrentPage || page.PerPage < 1 {
		return Result{}, WagoPageMeta{}, errf("hotfix_protocol_changed", "pagination", "invalid page metadata")
	}
	records := make([]Record, 0, len(page.Data))
	warnings := make([]string, 0)
	for _, wr := range page.Data {
		if wr.ID < 0 || wr.PushID < 0 || wr.RecordID < 0 || wr.RegionID < 0 || wr.Status < 0 || wr.Status > 255 {
			return Result{}, WagoPageMeta{}, errf("hotfix_protocol_changed", "record", "invalid numeric field")
		}
		created := time.Time{}
		if wr.CreatedAt != "" {
			created, _ = time.Parse("2006-01-02 15:04:05", wr.CreatedAt)
			if created.IsZero() {
				created, _ = time.Parse(time.RFC3339, wr.CreatedAt)
			}
		}
		records = append(records, Record{ID: uint64(wr.ID), Product: q.Product, PushID: int32(wr.PushID), RecordID: uint32(wr.RecordID), Status: uint8(wr.Status), Build: string(wr.Build), Region: uint32(wr.RegionID), Locale: wr.Locale, TableName: wr.TableName, CreatedAt: created, Data: wr.Data})
		if wr.Status < 1 || wr.Status > 4 {
			warnings = append(warnings, fmt.Sprintf("hotfix_unknown_status:%d", wr.Status))
		}
	}
	filtered, err := Filter(records, q)
	if err != nil {
		return Result{}, WagoPageMeta{}, err
	}
	if len(filtered) == 0 && len(records) > 0 {
		warnings = append(warnings, "wago_search_candidates_did_not_match_exact_filters")
	}
	sum := sha256.Sum256(raw)
	meta := WagoPageMeta{CurrentPage: page.CurrentPage, LastPage: page.LastPage, PerPage: page.PerPage, Total: int64(page.Total), NextPageURL: page.NextPageURL, ResponseSHA256: hex.EncodeToString(sum[:]), EmbeddedJSON: append([]byte(nil), embedded...), CapturedAt: time.Now().UTC()}
	cov := Coverage{Source: "wago", Build: q.Build, Region: q.Region, Locale: q.Locale, Complete: page.CurrentPage == 1 && page.LastPage == 1, StartPage: page.CurrentPage, EndPage: page.CurrentPage, Total: int64(page.Total), LastPage: page.LastPage, CapturedAt: meta.CapturedAt, Ref: meta.ResponseSHA256}
	return Result{Query: q, Source: "wago", Coverage: cov, Records: filtered, Warnings: warnings, Page: page.CurrentPage, LastPage: page.LastPage, Total: int64(page.Total)}, meta, nil
}

func findInertiaPage(raw []byte) ([]byte, error) {
	z := xhtml.NewTokenizer(bytes.NewReader(raw))
	for {
		tt := z.Next()
		switch tt {
		case xhtml.ErrorToken:
			if z.Err() != nil {
				return nil, errf("hotfix_protocol_changed", "data-page", "div#app not found")
			}
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			t := z.Token()
			if !strings.EqualFold(t.Data, "div") {
				continue
			}
			id, page := "", ""
			for _, a := range t.Attr {
				switch strings.ToLower(a.Key) {
				case "id":
					id = a.Val
				case "data-page":
					page = a.Val
				}
			}
			if id == "app" && page != "" {
				return []byte(stdhtml.UnescapeString(page)), nil
			}
		}
	}
}

func ValidatePageSequence(first, next WagoPageMeta) error {
	if next.Total != first.Total || next.LastPage != first.LastPage {
		return errf("hotfix_coverage_incomplete", "pagination", "total/last_page drift: first=%d/%d next=%d/%d", first.Total, first.LastPage, next.Total, next.LastPage)
	}
	if next.CurrentPage <= first.CurrentPage {
		return errf("hotfix_protocol_changed", "pagination", "duplicate or reversed page %d", next.CurrentPage)
	}
	if next.CurrentPage != first.CurrentPage+1 {
		return errf("hotfix_coverage_incomplete", "pagination", "missing page between %d and %d", first.CurrentPage, next.CurrentPage)
	}
	if first.CurrentPage < first.LastPage {
		if strings.TrimSpace(first.NextPageURL) == "" {
			return errf("hotfix_coverage_incomplete", "pagination", "missing next_page_url after page %d", first.CurrentPage)
		}
		u, err := url.Parse(first.NextPageURL)
		if err != nil {
			return errf("hotfix_protocol_changed", "next_page_url", "invalid URL: %v", err)
		}
		p, err := strconv.Atoi(u.Query().Get("page"))
		if err != nil || p != next.CurrentPage {
			return errf("hotfix_coverage_incomplete", "next_page_url", "page %d points to %q, expected page %d", first.CurrentPage, first.NextPageURL, next.CurrentPage)
		}
	}
	return nil
}

var _ = fmt.Sprint

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type WagoSource struct {
	BaseURL  string
	Client   HTTPDoer
	MaxPages int
	CacheDir string
}

func NewWagoSource(client HTTPDoer) *WagoSource {
	if client == nil {
		client = http.DefaultClient
	}
	return &WagoSource{BaseURL: "https://wago.tools", Client: client, MaxPages: 2048}
}

func (s *WagoSource) WithCache(dir string) *WagoSource {
	s.CacheDir = strings.TrimSpace(dir)
	return s
}
func (s *WagoSource) Query(ctx context.Context, q Query) (Result, error) {
	if err := q.Validate(); err != nil {
		return Result{}, err
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	search := strings.TrimSpace(q.Search)
	if search == "" {
		switch {
		case q.RecordID != nil:
			search = strconv.FormatUint(uint64(*q.RecordID), 10)
		case q.PushID != nil:
			search = strconv.FormatInt(int64(*q.PushID), 10)
		default:
			parts := strings.Split(q.Build, ".")
			search = parts[len(parts)-1]
		}
	}
	first, meta, err := s.fetch(ctx, q, page, search)
	if err != nil {
		return Result{}, err
	}
	all := append([]Record(nil), first.Records...)
	warnings := append([]string(nil), first.Warnings...)
	seen := map[uint64]struct{}{}
	for _, r := range all {
		if r.ID != 0 {
			seen[r.ID] = struct{}{}
		}
	}
	end := meta.CurrentPage
	if q.Page == 0 && (q.Latest || q.Limit == 0) && meta.LastPage > meta.CurrentPage {
		max := s.MaxPages
		if max <= 0 {
			max = 2048
		}
		if meta.LastPage-meta.CurrentPage+1 > max {
			return Result{}, errf("hotfix_coverage_incomplete", "pagination", "candidate set requires %d pages, limit is %d", meta.LastPage-meta.CurrentPage+1, max)
		}
		prev := meta
		for p := meta.CurrentPage + 1; p <= meta.LastPage; p++ {
			next, nm, e := s.fetch(ctx, q, p, search)
			if e != nil {
				return Result{}, e
			}
			if e = ValidatePageSequence(prev, nm); e != nil {
				return Result{}, e
			}
			for _, r := range next.Records {
				if r.ID != 0 {
					if _, ok := seen[r.ID]; ok {
						return Result{}, errf("hotfix_protocol_changed", "record.id", "duplicate Wago id %d", r.ID)
					}
					seen[r.ID] = struct{}{}
				}
				all = append(all, r)
			}
			warnings = append(warnings, next.Warnings...)
			prev = nm
			end = p
		}
	}
	filtered, err := Filter(all, q)
	if err != nil {
		return Result{}, err
	}
	complete := page == 1 && end == meta.LastPage
	return Result{Query: q, Source: "wago", Coverage: Coverage{Source: "wago", Build: q.Build, Region: q.Region, Locale: q.Locale, Complete: complete, StartPage: page, EndPage: end, Total: meta.Total, LastPage: meta.LastPage, CapturedAt: meta.CapturedAt, Ref: meta.ResponseSHA256}, Records: filtered, Warnings: warnings, Page: page, LastPage: meta.LastPage, Total: meta.Total}, nil
}

type wagoCacheIdentity struct {
	Provider      string  `json:"provider"`
	ParserVersion string  `json:"parser_version"`
	Product       string  `json:"product"`
	Build         string  `json:"build"`
	Region        uint32  `json:"region"`
	Locale        string  `json:"locale"`
	Table         string  `json:"table,omitempty"`
	TableHash     *uint32 `json:"table_hash,omitempty"`
	RecordID      *uint32 `json:"record_id,omitempty"`
	PushID        *int32  `json:"push_id,omitempty"`
	Status        *uint8  `json:"status,omitempty"`
	Page          int     `json:"page"`
	Search        string  `json:"search,omitempty"`
}

type wagoCacheReceipt struct {
	Schema         string            `json:"schema"`
	Identity       wagoCacheIdentity `json:"identity"`
	RequestURL     string            `json:"request_url"`
	ResponseSHA256 string            `json:"response_sha256"`
	CapturedAt     time.Time         `json:"captured_at"`
	CurrentPage    int               `json:"current_page"`
	LastPage       int               `json:"last_page"`
	Total          int64             `json:"total"`
	NextPageURL    string            `json:"next_page_url,omitempty"`
}

func (s *WagoSource) fetch(ctx context.Context, q Query, page int, search string) (Result, WagoPageMeta, error) {
	base := strings.TrimRight(s.BaseURL, "/") + "/hotfixes"
	u, err := url.Parse(base)
	if err != nil {
		return Result{}, WagoPageMeta{}, err
	}
	v := u.Query()
	v.Set("page", strconv.Itoa(page))
	if search != "" {
		v.Set("search", search)
	}
	u.RawQuery = v.Encode()
	identity := wagoCacheIdentity{Provider: "wago", ParserVersion: WagoParserVersion, Product: q.Product, Build: q.Build, Region: q.Region, Locale: q.Locale, Table: q.Table, TableHash: q.TableHash, RecordID: q.RecordID, PushID: q.PushID, Status: q.Status, Page: page, Search: search}
	keyBytes, _ := json.Marshal(identity)
	keySum := sha256.Sum256(keyBytes)
	requestKey := hex.EncodeToString(keySum[:])
	if s.CacheDir != "" {
		if result, meta, ok := s.readCache(q, identity, requestKey); ok {
			return result, meta, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, WagoPageMeta{}, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := s.Client.Do(req)
	if err != nil {
		return Result{}, WagoPageMeta{}, errf("hotfix_unavailable", "wago", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, WagoPageMeta{}, errf("hotfix_unavailable", "wago", "HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return Result{}, WagoPageMeta{}, errf("hotfix_unavailable", "wago", err.Error())
	}
	result, meta, err := ParseWagoHTML(raw, q)
	if err != nil {
		return Result{}, WagoPageMeta{}, err
	}
	meta.RequestKey = requestKey
	if s.CacheDir != "" {
		receipt := wagoCacheReceipt{Schema: "wowdata.wago-cache.v1", Identity: identity, RequestURL: u.String(), ResponseSHA256: meta.ResponseSHA256, CapturedAt: meta.CapturedAt, CurrentPage: meta.CurrentPage, LastPage: meta.LastPage, Total: meta.Total, NextPageURL: meta.NextPageURL}
		if err := s.writeCache(requestKey, raw, receipt); err != nil {
			return Result{}, WagoPageMeta{}, errf("hotfix_corrupt_cache", "wago_cache", "%v", err)
		}
	}
	return result, meta, nil
}

func (s *WagoSource) readCache(q Query, identity wagoCacheIdentity, key string) (Result, WagoPageMeta, bool) {
	receiptBytes, err := os.ReadFile(filepath.Join(s.CacheDir, key+".json"))
	if err != nil {
		return Result{}, WagoPageMeta{}, false
	}
	var receipt wagoCacheReceipt
	if json.Unmarshal(receiptBytes, &receipt) != nil || receipt.Schema != "wowdata.wago-cache.v1" || !reflect.DeepEqual(receipt.Identity, identity) {
		return Result{}, WagoPageMeta{}, false
	}
	raw, err := os.ReadFile(filepath.Join(s.CacheDir, key+".html"))
	if err != nil {
		return Result{}, WagoPageMeta{}, false
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != receipt.ResponseSHA256 {
		return Result{}, WagoPageMeta{}, false
	}
	result, meta, err := ParseWagoHTML(raw, q)
	if err != nil {
		return Result{}, WagoPageMeta{}, false
	}
	meta.CapturedAt = receipt.CapturedAt
	meta.CacheHit = true
	meta.RequestKey = key
	result.Coverage.CapturedAt = receipt.CapturedAt
	result.Coverage.Ref = receipt.ResponseSHA256
	result.Warnings = append(result.Warnings, "wago_cache_hit")
	return result, meta, true
}

func (s *WagoSource) writeCache(key string, raw []byte, receipt wagoCacheReceipt) error {
	if err := os.MkdirAll(s.CacheDir, 0755); err != nil {
		return err
	}
	receiptBytes, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(s.CacheDir, key+".html"), raw); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.CacheDir, key+".json"), append(receiptBytes, '\n'))
}

func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hotfix-cache-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	backup := path + ".old"
	_ = os.Remove(backup)
	if _, err = os.Stat(path); err == nil {
		if err = os.Rename(path, backup); err != nil {
			return err
		}
	}
	if err = os.Rename(name, path); err != nil {
		_ = os.Rename(backup, path)
		return err
	}
	_ = os.Remove(backup)
	ok = true
	return nil
}
