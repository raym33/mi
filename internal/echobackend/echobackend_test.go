package echobackend

import (
	"context"
	"strings"
	"testing"

	"github.com/raym33/mi/internal/protocol"
)

func TestEchoChatStreamsAndEchoesPrompt(t *testing.T) {
	b := New()
	if b.Name() != "echo" {
		t.Fatalf("name = %q, want echo", b.Name())
	}

	var got strings.Builder
	chunks := 0
	done, err := b.Chat(context.Background(), protocol.InferRequest{
		Messages: []protocol.ProtocolMessage{
			{Role: "system", Content: "be brief"},
			{Role: "user", Content: "hola mundo"},
		},
	}, func(s string) error {
		chunks++
		got.WriteString(s)
		return nil
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if chunks < 2 {
		t.Fatalf("expected multiple streamed chunks, got %d", chunks)
	}
	if !strings.Contains(got.String(), "hola mundo") {
		t.Fatalf("reply did not echo the prompt: %q", got.String())
	}
	if done.FinishReason != "stop" || done.OutputTokens <= 0 || done.PromptTokens <= 0 {
		t.Fatalf("unexpected done: %+v", done)
	}
}

func TestEchoChatRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New().Chat(ctx, protocol.InferRequest{
		Messages: []protocol.ProtocolMessage{{Role: "user", Content: "x y z"}},
	}, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestEchoEmbedReturnsDeterministicVectors(t *testing.T) {
	b := New()
	first, err := b.Embed(context.Background(), "demo", []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	second, err := b.Embed(context.Background(), "demo", []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("second embed: %v", err)
	}
	if len(first.Vectors) != 2 {
		t.Fatalf("vectors = %d, want 2", len(first.Vectors))
	}
	if first.PromptTokens <= 0 {
		t.Fatalf("prompt tokens = %d, want > 0", first.PromptTokens)
	}
	for i, vector := range first.Vectors {
		if len(vector) != 8 {
			t.Fatalf("vector %d dim = %d, want 8", i, len(vector))
		}
		nonZero := false
		for j, value := range vector {
			if value != second.Vectors[i][j] {
				t.Fatalf("vector %d[%d] = %f, second = %f; want deterministic", i, j, value, second.Vectors[i][j])
			}
			if value != 0 {
				nonZero = true
			}
		}
		if !nonZero {
			t.Fatalf("vector %d is all zero", i)
		}
	}
}

func TestEchoEmbedRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New().Embed(ctx, "demo", []string{"alpha"})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}
