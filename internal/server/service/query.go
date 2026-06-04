package service

import (
	"context"
	"fmt"
)

type RequestContext struct {
	Region   string
	Product  string
	Locale   string
	BuildKey string
}

type QueryRowsRequest struct {
	Context RequestContext
	Table   string
	IDs     []uint64
	IDField string
	Fields  []string
	Filter  string
	Limit   int
	Offset  int
}

type SearchRequest struct {
	Context RequestContext
	Table   string
	Field   string
	Query   string
	Limit   int
}

type ForeignKeyRequest struct {
	Context RequestContext
	Table   string
	Field   string
	Value   interface{}
	Limit   int
}

type StreamRequest struct {
	Context RequestContext
	Table   string
	Fields  []string
	Limit   int
	Offset  int
}

type SchemaRequest struct {
	Context RequestContext
	Table   string
}

type TablesRequest struct {
	Context RequestContext
}

type Schema struct {
	Table    string
	RowCount int
	Fields   []Field
}

type Field struct {
	Name string
	Type string
}

type TableCatalog struct {
	Tables []TableInfo
}

type TableInfo struct {
	Name string `json:"name"`
}

type QueryService interface {
	Schema(context.Context, SchemaRequest) (Schema, error)
	Tables(context.Context, TablesRequest) (TableCatalog, error)
	Rows(context.Context, QueryRowsRequest) ([]map[string]interface{}, error)
	Search(context.Context, SearchRequest) ([]map[string]interface{}, error)
	ForeignKey(context.Context, ForeignKeyRequest) ([]map[string]interface{}, error)
	Stream(context.Context, StreamRequest) ([]map[string]interface{}, error)
}

type CapabilityUnavailableError struct {
	Code       string
	Capability string
	Message    string
}

func (e CapabilityUnavailableError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Capability != "" {
		return fmt.Sprintf("%s is unavailable in the HTTP service", e.Capability)
	}
	return "capability is unavailable in the HTTP service"
}

type UnavailableQueryService struct{}

func (UnavailableQueryService) Schema(context.Context, SchemaRequest) (Schema, error) {
	return Schema{}, newCapabilityUnavailableError("query engine")
}

func (UnavailableQueryService) Tables(context.Context, TablesRequest) (TableCatalog, error) {
	return TableCatalog{}, newCapabilityUnavailableError("query engine")
}

func (UnavailableQueryService) Rows(context.Context, QueryRowsRequest) ([]map[string]interface{}, error) {
	return nil, newCapabilityUnavailableError("query engine")
}

func (UnavailableQueryService) Search(context.Context, SearchRequest) ([]map[string]interface{}, error) {
	return nil, newCapabilityUnavailableError("query engine")
}

func (UnavailableQueryService) ForeignKey(context.Context, ForeignKeyRequest) ([]map[string]interface{}, error) {
	return nil, newCapabilityUnavailableError("query engine")
}

func (UnavailableQueryService) Stream(context.Context, StreamRequest) ([]map[string]interface{}, error) {
	return nil, newCapabilityUnavailableError("query engine")
}

func newCapabilityUnavailableError(capability string) CapabilityUnavailableError {
	return CapabilityUnavailableError{
		Code:       "query_engine_unavailable",
		Capability: capability,
		Message:    fmt.Sprintf("%s is unavailable in the HTTP service", capability),
	}
}
