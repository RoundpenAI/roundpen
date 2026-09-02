package llmgw

import "time"

// Upstream is a provider endpoint and real API key (vault row).
type Upstream struct {
	Provider      string            `json:"provider"`
	BaseURL       string            `json:"base_url"`
	APIKey        string            `json:"-"`
	ModelMap      map[string]string `json:"model_map"`
	ModelPatterns []ModelPattern    `json:"model_patterns"`
	Enabled       bool              `json:"enabled"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// ModelPattern maps downstream model names via glob or regex.
type ModelPattern struct {
	Pattern string `json:"pattern"`
	Regex   bool   `json:"regex"`
	Target  string `json:"target"`
}

// VirtualKey is a client-facing credential replaced with the upstream key on relay.
type VirtualKey struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// TransactionBodies holds truncated request/response payloads for a log row.
type TransactionBodies struct {
	Request                  string `json:"request"`
	Response                 string `json:"response"`
	UpstreamRequest          string `json:"upstream_request,omitempty"`
	RequestTruncated         bool   `json:"request_truncated"`
	ResponseTruncated        bool   `json:"response_truncated"`
	UpstreamRequestTruncated bool   `json:"upstream_request_truncated,omitempty"`
}

// Transaction is one relay attempt (success or failure).
type Transaction struct {
	ID            int64     `json:"id"`
	RequestID     string    `json:"request_id"`
	VirtualKey    string    `json:"virtual_key"`
	VirtualName   string    `json:"virtual_name"`
	Provider      string    `json:"provider"`
	Method        string    `json:"method"`
	Path          string    `json:"path"`
	UpstreamURL   string    `json:"upstream_url,omitempty"`
	StatusCode    int       `json:"status_code"`
	RequestBytes  int64     `json:"request_bytes"`
	ResponseBytes int64     `json:"response_bytes"`
	DurationMS    int64     `json:"duration_ms"`
	Error         string    `json:"error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// ListOptions filters transaction queries.
type ListOptions struct {
	VirtualKey string
	Provider   string
	Limit      int
	Offset     int
}

// Stats aggregates relay traffic.
type Stats struct {
	TotalRequests      int64   `json:"total_requests"`
	TotalRequestBytes  int64   `json:"total_request_bytes"`
	TotalResponseBytes int64   `json:"total_response_bytes"`
	AvgDurationMS      float64 `json:"avg_duration_ms"`
	ErrorCount         int64   `json:"error_count"`
}
