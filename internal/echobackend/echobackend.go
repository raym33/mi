// Package echobackend is a dependency-free inference backend for demos and
// tests. It streams a deterministic reply that echoes the user's prompt, so the
// full coordinator + node-agent path can be exercised without Ollama or a GPU.
package echobackend

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/raym33/mi/internal/protocol"
)

type Backend struct{}

func New() *Backend { return &Backend{} }

func (b *Backend) Name() string { return "echo" }

func (b *Backend) Chat(ctx context.Context, req protocol.InferRequest, onChunk func(string) error) (protocol.InferDone, error) {
	reply := "mi echo node online. you said: " + lastUserMessage(req)

	promptChars := 0
	for _, msg := range req.Messages {
		promptChars += utf8.RuneCountInString(msg.Content)
	}

	// Stream word by word so streaming and time-to-first-token behave like a
	// real backend.
	words := strings.Fields(reply)
	for i, word := range words {
		if err := ctx.Err(); err != nil {
			return protocol.InferDone{}, err
		}
		chunk := word
		if i < len(words)-1 {
			chunk += " "
		}
		if err := onChunk(chunk); err != nil {
			return protocol.InferDone{}, err
		}
	}

	return protocol.InferDone{
		FinishReason: "stop",
		PromptTokens: estimateTokens(promptChars),
		OutputTokens: estimateTokens(utf8.RuneCountInString(reply)),
	}, nil
}

func lastUserMessage(req protocol.InferRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(req.Messages[i].Role, "user") {
			return req.Messages[i].Content
		}
	}
	if len(req.Messages) > 0 {
		return req.Messages[len(req.Messages)-1].Content
	}
	return ""
}

func estimateTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}
