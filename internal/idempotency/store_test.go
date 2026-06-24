package idempotency

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBeginTracksInProgressAndCompletedRecords(t *testing.T) {
	store := newTestStore(t, time.Hour)
	defer store.Close()

	rec, err := store.Begin("key-1")
	if err != nil {
		t.Fatalf("begin new key: %v", err)
	}
	if rec != nil {
		t.Fatalf("begin new key record = %+v, want nil owner", rec)
	}

	rec, err = store.Begin("key-1")
	if err != nil {
		t.Fatalf("begin in-progress key: %v", err)
	}
	if rec == nil || rec.Status != "in_progress" {
		t.Fatalf("begin in-progress record = %+v, want in_progress", rec)
	}

	body := []byte(`{"ok":true}`)
	if err := store.Complete("key-1", 200, "application/json", body); err != nil {
		t.Fatalf("complete key: %v", err)
	}

	rec, err = store.Begin("key-1")
	if err != nil {
		t.Fatalf("begin completed key: %v", err)
	}
	if rec == nil || rec.Status != "completed" || rec.StatusCode != 200 || rec.ContentType != "application/json" || string(rec.Response) != string(body) {
		t.Fatalf("completed record = %+v, want stored response", rec)
	}
}

func TestAbortMakesKeyClaimableAgain(t *testing.T) {
	store := newTestStore(t, time.Hour)
	defer store.Close()

	if rec, err := store.Begin("key-1"); err != nil || rec != nil {
		t.Fatalf("begin new key record=%+v err=%v, want owner", rec, err)
	}
	if err := store.Abort("key-1"); err != nil {
		t.Fatalf("abort key: %v", err)
	}
	if rec, err := store.Begin("key-1"); err != nil || rec != nil {
		t.Fatalf("begin aborted key record=%+v err=%v, want owner", rec, err)
	}
}

func TestStaleInProgressIsClaimable(t *testing.T) {
	store := newTestStore(t, time.Nanosecond)
	defer store.Close()

	if rec, err := store.Begin("key-1"); err != nil || rec != nil {
		t.Fatalf("begin new key record=%+v err=%v, want owner", rec, err)
	}
	time.Sleep(time.Millisecond)
	if rec, err := store.Begin("key-1"); err != nil || rec != nil {
		t.Fatalf("begin stale key record=%+v err=%v, want owner", rec, err)
	}
}

func TestDisabledStoreIsNoop(t *testing.T) {
	store, err := New("", 0)
	if err != nil {
		t.Fatalf("new disabled store: %v", err)
	}
	if store != nil {
		t.Fatalf("disabled store = %+v, want nil", store)
	}

	var nilStore *Store
	if nilStore.Enabled() {
		t.Fatal("nil store should not be enabled")
	}
	if rec, err := nilStore.Begin("key-1"); err != nil || rec != nil {
		t.Fatalf("nil begin record=%+v err=%v, want nil nil", rec, err)
	}
	if err := nilStore.Complete("key-1", 200, "application/json", []byte("ok")); err != nil {
		t.Fatalf("nil complete: %v", err)
	}
	if err := nilStore.Abort("key-1"); err != nil {
		t.Fatalf("nil abort: %v", err)
	}
	if err := nilStore.Close(); err != nil {
		t.Fatalf("nil close: %v", err)
	}
}

func newTestStore(t *testing.T, ttl time.Duration) *Store {
	t.Helper()
	store, err := New(filepath.Join(t.TempDir(), "idempotency.db"), ttl)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}
