package challenge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raym33/mi/internal/config"
)

func TestLedgerRecordsSummariesAndVerifies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "challenges.jsonl")
	ledger, err := New(config.ChallengeConfig{Enabled: true, Path: path})
	if err != nil {
		t.Fatalf("new ledger: %v", err)
	}
	if _, err := ledger.Record(RecordInput{ProviderID: "provider-a", NodeID: "node-a", Challenge: "latency", Passed: true, Score: 90, LatencyMs: 120}); err != nil {
		t.Fatalf("record pass: %v", err)
	}
	if _, err := ledger.Record(RecordInput{ProviderID: "provider-a", NodeID: "node-a", Challenge: "correctness", Passed: false, Score: 20, LatencyMs: 200}); err != nil {
		t.Fatalf("record fail: %v", err)
	}
	snapshot := ledger.Snapshot(10)
	if snapshot.Events != 2 || len(snapshot.Summaries) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	summary := snapshot.Summaries[0]
	if summary.Challenges != 2 || summary.Passed != 1 || summary.Failed != 1 || summary.PassRateBPS != 5000 || summary.AverageScore != 55 {
		t.Fatalf("summary = %+v, want pass/fail averages", summary)
	}
	if verification := ledger.Verify(); !verification.Valid || verification.Events != 2 {
		t.Fatalf("verify = %+v", verification)
	}
}

func TestLedgerDetectsTamperingOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "challenges.jsonl")
	ledger, err := New(config.ChallengeConfig{Enabled: true, Path: path})
	if err != nil {
		t.Fatalf("new ledger: %v", err)
	}
	if _, err := ledger.Record(RecordInput{ProviderID: "provider-a", Challenge: "latency", Passed: true, Score: 90}); err != nil {
		t.Fatalf("record: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	tampered := strings.Replace(string(data), `"score":90`, `"score":10`, 1)
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatalf("write tampered ledger: %v", err)
	}
	if _, err := New(config.ChallengeConfig{Enabled: true, Path: path}); err != ErrInvalidChain {
		t.Fatalf("new tampered ledger = %v, want ErrInvalidChain", err)
	}
}

func TestLedgerRejectsRecordsWhenDisabled(t *testing.T) {
	ledger, err := New(config.ChallengeConfig{})
	if err != nil {
		t.Fatalf("new ledger: %v", err)
	}
	if _, err := ledger.Record(RecordInput{ProviderID: "provider-a", Challenge: "latency", Passed: true, Score: 90}); err != ErrDisabled {
		t.Fatalf("record disabled = %v, want ErrDisabled", err)
	}
}
