package hotfix

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Error struct {
	Code    string
	Message string
	Field   string
}

func (e *Error) Error() string {
	if e.Field == "" {
		return e.Code + ": " + e.Message
	}
	return e.Code + ": " + e.Field + ": " + e.Message
}

func errf(code, field, format string, args ...any) error {
	return &Error{Code: code, Field: field, Message: fmt.Sprintf(format, args...)}
}

type Query struct {
	Product   string
	Build     string
	Region    uint32
	Locale    string
	Table     string
	TableHash *uint32
	RecordID  *uint32
	PushID    *int32
	Status    *uint8
	From, To  *time.Time
	Search    string
	Source    string
	Raw       bool
	Decoded   bool
	Latest    bool
	Page      int
	Limit     int
}

func (q Query) Validate() error {
	if strings.TrimSpace(q.Product) == "" {
		return errf("hotfix_invalid_query", "product", "product is required")
	}
	if strings.TrimSpace(q.Build) == "" {
		return errf("hotfix_invalid_query", "build", "build is required")
	}
	if q.Region == 0 {
		return errf("hotfix_invalid_query", "region", "region is required")
	}
	if strings.TrimSpace(q.Locale) == "" {
		return errf("hotfix_invalid_query", "locale", "locale is required")
	}
	if q.Table == "" && q.TableHash == nil && q.RecordID == nil && q.PushID == nil && q.Search == "" && !q.Latest {
		return errf("hotfix_invalid_query", "", "at least one table, id, push, search, or latest filter is required")
	}
	if q.Page < 0 {
		return errf("hotfix_invalid_query", "page", "page must be non-negative")
	}
	if q.Limit < 0 {
		return errf("hotfix_invalid_query", "limit", "limit must be non-negative")
	}
	if q.From != nil && q.To != nil && q.From.After(*q.To) {
		return errf("hotfix_invalid_query", "time", "from must not be after to")
	}
	return nil
}

type Record struct {
	ID            uint64    `json:"id,omitempty"`
	Product       string    `json:"product,omitempty"`
	PushID        int32     `json:"push_id"`
	UniqueID      uint32    `json:"unique_id,omitempty"`
	RecordID      uint32    `json:"record_id"`
	TableHash     uint32    `json:"table_hash"`
	TableName     string    `json:"table_name,omitempty"`
	Status        uint8     `json:"status"`
	Build         string    `json:"build"`
	Region        uint32    `json:"region_id"`
	Locale        string    `json:"locale"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	RawData       []byte    `json:"raw_data,omitempty"`
	Data          any       `json:"data,omitempty"`
	PayloadOffset int64     `json:"payload_offset,omitempty"`
	PayloadLength int       `json:"payload_length,omitempty"`
}

type Coverage struct {
	Source     string    `json:"source"`
	Build      string    `json:"build"`
	Region     uint32    `json:"region_id"`
	Locale     string    `json:"locale"`
	Complete   bool      `json:"complete"`
	StartPage  int       `json:"start_page,omitempty"`
	EndPage    int       `json:"end_page,omitempty"`
	Total      int64     `json:"total,omitempty"`
	LastPage   int       `json:"last_page,omitempty"`
	CapturedAt time.Time `json:"captured_at,omitempty"`
	Ref        string    `json:"ref,omitempty"`
}

type Result struct {
	Query    Query    `json:"query"`
	Source   string   `json:"source"`
	Coverage Coverage `json:"coverage"`
	Records  []Record `json:"records"`
	Warnings []string `json:"warnings,omitempty"`
	Page     int      `json:"page,omitempty"`
	LastPage int      `json:"last_page,omitempty"`
	Total    int64    `json:"total,omitempty"`
}

type Source interface {
	Query(context.Context, Query) (Result, error)
}

func Filter(records []Record, q Query) ([]Record, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	out := records[:0]
	for _, r := range records {
		if q.Build != "" && !buildMatches(q.Build, r.Build) || q.Region != 0 && r.Region != q.Region || q.Locale != "" && !strings.EqualFold(r.Locale, q.Locale) {
			continue
		}
		if q.Table != "" && !strings.EqualFold(r.TableName, q.Table) {
			continue
		}
		if q.TableHash != nil && r.TableHash != *q.TableHash {
			continue
		}
		if q.RecordID != nil && r.RecordID != *q.RecordID {
			continue
		}
		if q.PushID != nil && r.PushID != *q.PushID {
			continue
		}
		if q.Status != nil && r.Status != *q.Status {
			continue
		}
		if q.From != nil && r.CreatedAt.Before(*q.From) {
			continue
		}
		if q.To != nil && r.CreatedAt.After(*q.To) {
			continue
		}
		out = append(out, r)
	}
	return orderRecords(out, q), nil
}

func orderRecords(out []Record, q Query) []Record {
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PushID != out[j].PushID {
			return out[i].PushID < out[j].PushID
		}
		if out[i].RecordID != out[j].RecordID {
			return out[i].RecordID < out[j].RecordID
		}
		return out[i].ID < out[j].ID
	})
	if q.Latest && len(out) > 0 {
		max := out[len(out)-1].PushID
		start := len(out)
		for i := len(out) - 1; i >= 0; i-- {
			if out[i].PushID != max {
				break
			}
			start = i
		}
		out = out[start:]
	}
	if q.Page > 0 && q.Limit > 0 {
		from := q.Page * q.Limit
		if from >= len(out) {
			return []Record{}
		}
		to := from + q.Limit
		if to > len(out) {
			to = len(out)
		}
		out = out[from:to]
	} else if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out
}

func buildMatches(query, record string) bool {
	if query == record {
		return true
	}
	return strings.HasSuffix(query, "."+record) || strings.HasSuffix(record, "."+query)
}

type MemorySource struct {
	Records  []Record
	Coverage Coverage
}

func (s MemorySource) Query(ctx context.Context, q Query) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, errf("hotfix_cancelled", "", ctx.Err().Error())
	default:
	}
	rows, err := Filter(append([]Record(nil), s.Records...), q)
	if err != nil {
		return Result{}, err
	}
	c := s.Coverage
	if c.Source == "" {
		c.Source = "fixture"
	}
	return Result{Query: q, Source: c.Source, Coverage: c, Records: rows, Total: int64(len(rows))}, nil
}
