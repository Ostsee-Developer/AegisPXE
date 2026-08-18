package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db     *sql.DB
	logger *slog.Logger
	now    func() time.Time
}

func Open(ctx context.Context, path string, logger *slog.Logger) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	s := &Store{db: db, logger: logger, now: time.Now}
	if err := s.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	logger.InfoContext(ctx, "storage ready", "component", "store", "operation", "open")
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }
