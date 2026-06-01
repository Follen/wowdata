package http

import "fmt"

type Status struct {
	OK       bool              `json:"ok"`
	Contexts []ContextStatus   `json:"contexts"`
	Cache    CacheStatus       `json:"cache"`
	Jobs     []JobStatus       `json:"jobs"`
	Memory   MemoryStatus      `json:"memory"`
	Errors   []StructuredError `json:"errors"`
}

type ContextStatus struct {
	Region   string `json:"region"`
	Product  string `json:"product"`
	Locale   string `json:"locale"`
	BuildKey string `json:"buildKey"`
	State    string `json:"state"`
}

type CacheStatus struct {
	Root     string `json:"root"`
	Pressure bool   `json:"pressure"`
}

type JobStatus struct {
	Key   string `json:"key"`
	State string `json:"state"`
}

type MemoryStatus struct {
	MaxContexts    int `json:"maxContexts"`
	ActiveContexts int `json:"activeContexts"`
}

type StructuredError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ContextDefaults struct {
	Region  string `json:"region"`
	Product string `json:"product"`
	Locale  string `json:"locale"`
}

type PinnedContext struct {
	Region  string `json:"region"`
	Product string `json:"product"`
	Locale  string `json:"locale"`
	Label   string `json:"label"`
}

type BuildCatalog struct {
	Default ContextDefaults `json:"default"`
	Pinned  []PinnedContext `json:"pinned"`
}

type CapabilityError struct {
	Code    string
	Message string
}

func (e CapabilityError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}
	return "capability unavailable"
}

func NewCapabilityError(code, capability string) CapabilityError {
	if code == "" {
		code = "query_engine_unavailable"
	}
	return CapabilityError{
		Code:    code,
		Message: fmt.Sprintf("%s is unavailable in the HTTP service", capability),
	}
}
