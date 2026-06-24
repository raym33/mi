package idempotency

import (
	"database/sql"
	"errors"
	"time"

	"github.com/raym33/mi/internal/sqlitestore"
)

const defaultTTL = 24 * time.Hour

type Record struct {
	Status      string
	StatusCode  int
	ContentType string
	Response    []byte
}

type Store struct {
	db  *sql.DB
	ttl time.Duration
}

func New(path string, ttl time.Duration) (*Store, error) {
	if path == "" {
		return nil, nil
	}
	if ttl <= 0 {
		ttl = defaultTTL
	}
	db, err := sqlitestore.Open(path)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db, ttl: ttl}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Enabled() bool {
	return s != nil && s.db != nil
}

func (s *Store) Close() error {
	if !s.Enabled() {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Begin(key string) (*Record, error) {
	if !s.Enabled() {
		return nil, nil
	}
	now := time.Now().UnixNano()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var record Record
	var createdAt int64
	err = tx.QueryRow(`
SELECT status, status_code, content_type, response, created_at
FROM idempotency_keys
WHERE key = ?`, key).Scan(&record.Status, &record.StatusCode, &record.ContentType, &record.Response, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := tx.Exec(`INSERT INTO idempotency_keys (key, status, created_at) VALUES (?, 'in_progress', ?)`, key, now); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if record.Status == "completed" {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &record, nil
	}
	if record.Status == "in_progress" && now-createdAt > s.ttl.Nanoseconds() {
		if _, err := tx.Exec(`UPDATE idempotency_keys SET created_at = ? WHERE key = ?`, now, key); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Record{Status: "in_progress"}, nil
}

func (s *Store) Complete(key string, statusCode int, contentType string, body []byte) error {
	if !s.Enabled() {
		return nil
	}
	_, err := s.db.Exec(`
UPDATE idempotency_keys
SET status = 'completed', status_code = ?, content_type = ?, response = ?, completed_at = ?
WHERE key = ?`, statusCode, contentType, body, time.Now().UnixNano(), key)
	return err
}

func (s *Store) Abort(key string) error {
	if !s.Enabled() {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM idempotency_keys WHERE key = ? AND status = 'in_progress'`, key)
	return err
}

func (s *Store) init() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS idempotency_keys (
	key TEXT PRIMARY KEY,
	status TEXT NOT NULL,
	status_code INTEGER NOT NULL DEFAULT 0,
	content_type TEXT NOT NULL DEFAULT '',
	response BLOB,
	created_at INTEGER NOT NULL,
	completed_at INTEGER NOT NULL DEFAULT 0
);
`)
	return err
}
