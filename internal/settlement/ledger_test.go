package settlement

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		TargetLatencyMs:              1000,
		LatencyPenaltyBPS:            1000,
	})
	if err != nil {
		t.Fatalf("new ledger: %v", err)
	}
	event, err := ledger.Record(RecordInput{
		RequestID:        "req-1",
		ConsumerID:       "studio",
		ProviderID:       "provider-a",
		NodeID:           "node-a",
		Model:            "fast",
		PrivacyTier:      "public",
		Done:             protocol.InferDone{PromptTokens: 400, OutputTokens: 600},
		Latency:          1500 * time.Millisecond,
		DispatchAttempts: 2,
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if event.Index != 1 || event.PreviousHash != genesisHash || event.Hash == "" {
		t.Fatalf("event hash chain fields = %+v", event)
	}
	if event.PriceMicros != 2000 || event.ProviderRewardMicros != 1260 || event.ProviderPenaltyMicros != 140 {
		t.Fatalf("prices = price %d reward %d penalty %d, want 2000/1260/140", event.PriceMicros, event.ProviderRewardMicros, event.ProviderPenaltyMicros)
	}
	if event.LatencyMs != 1500 || event.DispatchAttempts != 2 {
		t.Fatalf("latency/attempts = %d/%d, want 1500/2", event.LatencyMs, event.DispatchAttempts)
	}
	if verification := ledger.Verify(); !verification.Valid || verification.Events != 1 || verification.LastHash != event.Hash {
		t.Fatalf("verification = %+v", verification)
	}
	snapshot := ledger.Snapshot(10)
	if len(snapshot.ConsumerBalances) != 1 || snapshot.ConsumerBalances[0].DebitMicros != 2000 {
		t.Fatalf("consumer balances = %+v", snapshot.ConsumerBalances)
	}
	if len(snapshot.ProviderBalances) != 1 || snapshot.ProviderBalances[0].RewardMicros != 1260 || snapshot.ProviderBalances[0].PenaltyMicros != 140 {
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

func TestSQLiteLedgerRecordsAndVerifiesEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settlement.db")
	ledger, err := New(config.SettlementConfig{
		Enabled:                      true,
		SQLitePath:                   path,
		PricePerThousandTokensMicros: 2000,
		ProviderRewardShareBPS:       7000,
		TargetLatencyMs:              1000,
		LatencyPenaltyBPS:            1000,
	})
	if err != nil {
		t.Fatalf("new ledger: %v", err)
	}
	event, err := ledger.Record(RecordInput{
		RequestID:        "req-1",
		ConsumerID:       "studio",
		ProviderID:       "provider-a",
		NodeID:           "node-a",
		Model:            "fast",
		PrivacyTier:      "public",
		Done:             protocol.InferDone{PromptTokens: 400, OutputTokens: 600},
		Latency:          1500 * time.Millisecond,
		DispatchAttempts: 2,
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	var journalMode string
	if err := ledger.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode = %q, want wal", journalMode)
	}
	if err := ledger.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}

	restarted, err := New(config.SettlementConfig{
		Enabled:                      true,
		SQLitePath:                   path,
		PricePerThousandTokensMicros: 2000,
		ProviderRewardShareBPS:       7000,
		TargetLatencyMs:              1000,
		LatencyPenaltyBPS:            1000,
	})
	if err != nil {
		t.Fatalf("restart ledger: %v", err)
	}
	defer restarted.Close()
	if verification := restarted.Verify(); !verification.Valid || verification.Events != 1 || verification.LastHash != event.Hash {
		t.Fatalf("verification = %+v, want valid persisted chain", verification)
	}
	snapshot := restarted.Snapshot(10)
	if snapshot.SQLitePath != path || snapshot.ChainPath != "" {
		t.Fatalf("snapshot paths = sqlite %q chain %q, want sqlite only", snapshot.SQLitePath, snapshot.ChainPath)
	}
	if len(snapshot.ProviderBalances) != 1 || snapshot.ProviderBalances[0].RewardMicros != 1260 {
		t.Fatalf("provider balances = %+v", snapshot.ProviderBalances)
	}
}

func TestSQLiteLedgerDetectsTamperingOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settlement.db")
	ledger, err := New(config.SettlementConfig{Enabled: true, SQLitePath: path, PricePerThousandTokensMicros: 1000})
	if err != nil {
		t.Fatalf("new ledger: %v", err)
	}
	if _, err := ledger.Record(RecordInput{RequestID: "req-1", ConsumerID: "studio", Done: protocol.InferDone{PromptTokens: 1, OutputTokens: 1}}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := ledger.db.Exec(`UPDATE settlement_events SET total_tokens = 200 WHERE event_index = 1`); err != nil {
		t.Fatalf("tamper sqlite: %v", err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}
	if _, err := New(config.SettlementConfig{Enabled: true, SQLitePath: path, PricePerThousandTokensMicros: 1000}); err != ErrInvalidChain {
		t.Fatalf("new tampered sqlite ledger = %v, want ErrInvalidChain", err)
	}
}
