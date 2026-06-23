package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/raym33/mi/internal/protocol"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: http.DefaultClient}
}

type chatRequest struct {
	Model    string                     `json:"model"`
	Messages []protocol.ProtocolMessage `json:"messages"`
	Stream   bool                       `json:"stream"`
	Options  map[string]any             `json:"options,omitempty"`
}

type chatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
}

func (c *Client) Chat(ctx context.Context, req protocol.InferRequest, onChunk func(string) error) (protocol.InferDone, error) {
	options := map[string]any{}
	if req.Temperature != nil {
		options["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		options["num_predict"] = *req.MaxTokens
	}

	body, err := json.Marshal(chatRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Stream:   true,
		Options:  options,
	})
	if err != nil {
		return protocol.InferDone{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return protocol.InferDone{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return protocol.InferDone{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return protocol.InferDone{}, fmt.Errorf("ollama returned %s", resp.Status)
	}

	done := protocol.InferDone{FinishReason: "stop"}
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 8*1024*1024)
	for scanner.Scan() {
		var chunk chatResponse
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			return done, err
		}
		if chunk.Message.Content != "" {
			if err := onChunk(chunk.Message.Content); err != nil {
				return done, err
			}
		}
		if chunk.Done {
			if chunk.DoneReason != "" {
				done.FinishReason = chunk.DoneReason
			}
			done.PromptTokens = chunk.PromptEvalCount
			done.OutputTokens = chunk.EvalCount
		}
	}
	return done, scanner.Err()
}
