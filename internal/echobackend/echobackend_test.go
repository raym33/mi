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
