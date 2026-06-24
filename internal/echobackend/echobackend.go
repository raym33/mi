// Package echobackend is a dependency-free inference backend for demos and
// tests. It streams a deterministic reply that echoes the user's prompt, so the
// full coordinator + node-agent path can be exercised without Ollama or a GPU.
package echobackend

import (
	"context"
	"math"
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

func (b *Backend) Embed(ctx context.Context, model string, input []string) (protocol.EmbedResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.EmbedResult{}, err
	}

	vectors := make([][]float32, 0, len(input))
	promptChars := 0
	for _, text := range input {
		if err := ctx.Err(); err != nil {
			return protocol.EmbedResult{}, err
		}
		promptChars += utf8.RuneCountInString(text)
		vectors = append(vectors, embedString(text))
	}
	return protocol.EmbedResult{
		Vectors:      vectors,
		PromptTokens: estimateTokens(promptChars),
	}, nil
}

func embedString(text string) []float32 {
	vector := make([]float32, 8)
	for i, r := range text {
		vector[i%len(vector)] += float32(r) + 1
	}
	var sumSquares float64
	for _, value := range vector {
		sumSquares += float64(value * value)
	}
	if sumSquares == 0 {
		return vector
	}
	norm := float32(math.Sqrt(sumSquares))
	for i := range vector {
		vector[i] /= norm
	}
	return vector
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
