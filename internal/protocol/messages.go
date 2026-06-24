package protocol

import "time"

const Version = 1

type Envelope struct {
	Version    int           `json:"version,omitempty"`
	Type       string        `json:"type"`
	RequestID  string        `json:"request_id,omitempty"`
	Register   *Register     `json:"register,omitempty"`
	Heartbeat  *Heartbeat    `json:"heartbeat,omitempty"`
	Infer      *InferRequest `json:"infer,omitempty"`
	Embed      *EmbedRequest `json:"embed,omitempty"`
	Chunk      *InferChunk   `json:"chunk,omitempty"`
	Done       *InferDone    `json:"done,omitempty"`
	Embeddings *EmbedResult  `json:"embeddings,omitempty"`
	Error      *InferError   `json:"error,omitempty"`
}

type Register struct {
	ProtocolVersion int      `json:"protocol_version,omitempty"`
	NodeID          string   `json:"node_id"`
	ProviderID      string   `json:"provider_id,omitempty"`
	ProviderToken   string   `json:"provider_token,omitempty"`
	PublicName      string   `json:"public_name,omitempty"`
	City            string   `json:"city,omitempty"`
	PrivacyMode     string   `json:"privacy_mode,omitempty"`
	PrivacyTiers    []string `json:"privacy_tiers,omitempty"`
	Hostname        string   `json:"hostname"`
	Arch            string   `json:"arch"`
	OS              string   `json:"os"`
	Backend         string   `json:"backend,omitempty"`
	DeviceKind      string   `json:"device_kind,omitempty"`
	DeviceVendor    string   `json:"device_vendor,omitempty"`
	DeviceModel     string   `json:"device_model,omitempty"`
	SoC             string   `json:"soc,omitempty"`
	Accelerators    []string `json:"accelerators,omitempty"`
	PowerMode       string   `json:"power_mode,omitempty"`
	NetworkMode     string   `json:"network_mode,omitempty"`
	Models          []string `json:"models"`
	MaxConcurrent   int      `json:"max_concurrent"`
}

type Heartbeat struct {
	ProtocolVersion int       `json:"protocol_version,omitempty"`
	NodeID          string    `json:"node_id"`
	Models          []string  `json:"models"`
	ActiveRequests  int       `json:"active_requests"`
	QueueDepth      int       `json:"queue_depth"`
	MemoryFreeMB    uint64    `json:"memory_free_mb"`
	LoadAverage     float64   `json:"load_average"`
	ObservedAt      time.Time `json:"observed_at"`
}

type InferRequest struct {
	Model        string            `json:"model"`
	Messages     []ProtocolMessage `json:"messages"`
	Stream       bool              `json:"stream"`
	PrivacyTier  string            `json:"privacy_tier,omitempty"`
	Backend      string            `json:"backend,omitempty"`
	DeviceKind   string            `json:"device_kind,omitempty"`
	SoC          string            `json:"soc,omitempty"`
	Accelerators []string          `json:"accelerators,omitempty"`
	Temperature  *float64          `json:"temperature,omitempty"`
	MaxTokens    *int              `json:"max_tokens,omitempty"`
}

type ProtocolMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type EmbedRequest struct {
	Model       string   `json:"model"`
	Input       []string `json:"input"`
	PrivacyTier string   `json:"privacy_tier,omitempty"`
}

type EmbedResult struct {
	Vectors      [][]float32 `json:"vectors"`
	PromptTokens int         `json:"prompt_tokens"`
}

type InferChunk struct {
	Content string `json:"content"`
}

type InferDone struct {
	FinishReason string `json:"finish_reason"`
	PromptTokens int    `json:"prompt_tokens"`
	OutputTokens int    `json:"output_tokens"`
	KeyID        string `json:"key_id,omitempty"`
	Signature    string `json:"signature,omitempty"`
}

type InferError struct {
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}
