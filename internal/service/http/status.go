package http

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
