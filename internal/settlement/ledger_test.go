package settlement

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raym33/mi/internal/config"
	"github.com/raym33/mi/internal/protocol"
)

func TestLedgerRecordsAndVerifiesEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settlement.jsonl")
	ledger, err := New(config.SettlementConfig{
		Enabled:                      true,
		ChainPath:                    path,
		PricePerThousandTokensMicros: 2000,
		ProviderRewardShareBPS:       7000,
	})
	if err != nil {
		t.Fatalf("new ledger: %v", err)
	}
	event, err := ledger.Record(RecordInput{
		RequestID:   "req-1",
		ConsumerID:  "studio",
		ProviderID:  "provider-a",
		NodeID:      "node-a",
		Model:       "fast",
		PrivacyTier: "public",
		Done:        protocol.InferDone{PromptTokens: 400, OutputTokens: 600},
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if event.Index != 1 || event.PreviousHash != genesisHash || event.Hash == "" {
		t.Fatalf("event hash chain fields = %+v", event)
	}
	if event.PriceMicros != 2000 || event.ProviderRewardMicros != 1400 {
		t.Fatalf("prices = price %d reward %d, want 2000/1400", event.PriceMicros, event.ProviderRewardMicros)
	}
	if verification := ledger.Verify(); !verification.Valid || verification.Events != 1 || verification.LastHash != event.Hash {
		t.Fatalf("verification = %+v", verification)
	}
	snapshot := ledger.Snapshot(10)
	if len(snapshot.ConsumerBalances) != 1 || snapshot.ConsumerBalances[0].DebitMicros != 2000 {
		t.Fatalf("consumer balances = %+v", snapshot.ConsumerBalances)
	}
	if len(snapshot.ProviderBalances) != 1 || snapshot.ProviderBalances[0].RewardMicros != 1400 {
		t.Fatalf("provider balances = %+v", snapshot.ProviderBalances)
	}
}

func TestLedgerDetectsTamperingOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settlement.jsonl")
	ledger, err := New(config.SettlementConfig{Enabled: true, ChainPath: path, PricePerThousandTokensMicros: 1000})
	if err != nil {
		t.Fatalf("new ledger: %v", err)
	}
	if _, err := ledger.Record(RecordInput{RequestID: "req-1", ConsumerID: "studio", Done: protocol.InferDone{PromptTokens: 1, OutputTokens: 1}}); err != nil {
		t.Fatalf("record: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	tampered := strings.Replace(string(data), `"total_tokens":2`, `"total_tokens":200`, 1)
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatalf("write tampered ledger: %v", err)
	}
	if _, err := New(config.SettlementConfig{Enabled: true, ChainPath: path, PricePerThousandTokensMicros: 1000}); err != ErrInvalidChain {
		t.Fatalf("new tampered ledger = %v, want ErrInvalidChain", err)
	}
}
