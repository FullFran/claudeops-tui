package mcpserver

// SummaryResponse is the JSON response for claudeops_summary.
type SummaryResponse struct {
	Period        string  `json:"period"`
	Events        int64   `json:"events"`
	CostEUR       float64 `json:"cost_eur"`
	InTokens      int64   `json:"in_tokens"`
	OutTokens     int64   `json:"out_tokens"`
	CacheRead     int64   `json:"cache_read_tokens"`
	CacheCreate   int64   `json:"cache_create_tokens"`
	CacheHitRatio float64 `json:"cache_hit_ratio"`
}

// SessionResponse is a per-session row.
type SessionResponse struct {
	SessionID   string  `json:"session_id"`
	Project     string  `json:"project"`
	CostEUR     float64 `json:"cost_eur"`
	Events      int64   `json:"events"`
	FirstSeen   string  `json:"first_seen"`
	LastSeen    string  `json:"last_seen"`
	DurationSec float64 `json:"duration_seconds"`
}

// SessionDetailResponse is the response for claudeops_session_detail.
type SessionDetailResponse struct {
	Session SessionResponse  `json:"session"`
	Models  []ModelResponse  `json:"models"`
	Hourly  []HourlyResponse `json:"hourly"`
}

// ModelResponse is a per-model aggregate row.
type ModelResponse struct {
	Model         string  `json:"model"`
	Events        int64   `json:"events"`
	CostEUR       float64 `json:"cost_eur"`
	InTokens      int64   `json:"in_tokens"`
	OutTokens     int64   `json:"out_tokens"`
	CacheRead     int64   `json:"cache_read_tokens"`
	CacheCreate   int64   `json:"cache_create_tokens"`
	CacheHitRatio float64 `json:"cache_hit_ratio"`
}

// HourlyResponse is a per-hour aggregate row.
type HourlyResponse struct {
	Hour    int     `json:"hour"`
	CostEUR float64 `json:"cost_eur"`
	Events  int64   `json:"events"`
}

// ProjectResponse is a per-project cost row.
type ProjectResponse struct {
	Project string  `json:"project"`
	CostEUR float64 `json:"cost_eur"`
}

// DailyResponse is a per-day aggregate row.
type DailyResponse struct {
	Date     string  `json:"date"`
	CostEUR  float64 `json:"cost_eur"`
	Events   int64   `json:"events"`
	Sessions int64   `json:"sessions"`
}

// InsightResponse is a single derived insight.
type InsightResponse struct {
	ID             string `json:"id"`
	Severity       string `json:"severity"`
	Title          string `json:"title"`
	Detail         string `json:"detail"`
	Recommendation string `json:"recommendation"`
}
