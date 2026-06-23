package protocol

import "time"

type Envelope struct {
	Type      string        `json:"type"`
	RequestID string        `json:"request_id,omitempty"`
	Register  *Register     `json:"register,omitempty"`
	Heartbeat *Heartbeat    `json:"heartbeat,omitempty"`
	Infer     *InferRequest `json:"infer,omitempty"`
	Chunk     *InferChunk   `json:"chunk,omitempty"`
	Done      *InferDone    `json:"done,omitempty"`
	Error     *InferError   `json:"error,omitempty"`
}

type Register struct {
	NodeID        string   `json:"node_id"`
	ProviderID    string   `json:"provider_id,omitempty"`
	ProviderToken string   `json:"provider_token,omitempty"`
	PublicName    string   `json:"public_name,omitempty"`
	City          string   `json:"city,omitempty"`
	Hostname      string   `json:"hostname"`
	Arch          string   `json:"arch"`
	OS            string   `json:"os"`
	Models        []string `json:"models"`
	MaxConcurrent int      `json:"max_concurrent"`
}

type Heartbeat struct {
	NodeID         string    `json:"node_id"`
	Models         []string  `json:"models"`
	ActiveRequests int       `json:"active_requests"`
	QueueDepth     int       `json:"queue_depth"`
	MemoryFreeMB   uint64    `json:"memory_free_mb"`
	LoadAverage    float64   `json:"load_average"`
	ObservedAt     time.Time `json:"observed_at"`
}

type InferRequest struct {
	Model       string            `json:"model"`
	Messages    []ProtocolMessage `json:"messages"`
	Stream      bool              `json:"stream"`
	Temperature *float64          `json:"temperature,omitempty"`
	MaxTokens   *int              `json:"max_tokens,omitempty"`
}

type ProtocolMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type InferChunk struct {
	Content string `json:"content"`
}

type InferDone struct {
	FinishReason string `json:"finish_reason"`
	PromptTokens int    `json:"prompt_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

type InferError struct {
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}
