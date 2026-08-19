package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
)

const maxRecentEvents = 500

func appendEventTx(ctx context.Context, tx *sql.Tx, value event.Event) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO events(entity_type,entity_id,event_type,occurred_at,request_id,actor,message,error_code)
		VALUES(?,?,?,?,?,?,?,?)`, value.EntityType, value.EntityID, value.Type, value.OccurredAt.UTC().Format(time.RFC3339Nano), value.RequestID, value.Actor, value.Message, value.ErrorCode)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *Store) Events(ctx context.Context, entityType, entityID string) ([]event.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sequence,entity_type,entity_id,event_type,occurred_at,request_id,actor,message,error_code
		FROM events WHERE entity_type=? AND entity_id=? ORDER BY sequence`, entityType, entityID)
	if err != nil {
		return nil, s.storageError("query events", err)
	}
	defer rows.Close()
	return s.scanEvents(rows, false)
}

func (s *Store) RecentEvents(ctx context.Context, entityType, entityID string, limit int) ([]event.Event, error) {
	if limit <= 0 {
		return nil, nil
	}
	if limit > maxRecentEvents {
		limit = maxRecentEvents
	}
	rows, err := s.db.QueryContext(ctx, `SELECT sequence,entity_type,entity_id,event_type,occurred_at,request_id,actor,message,error_code
		FROM events WHERE entity_type=? AND entity_id=? ORDER BY sequence DESC LIMIT ?`, entityType, entityID, limit)
	if err != nil {
		return nil, s.storageError("query recent events", err)
	}
	defer rows.Close()
	return s.scanEvents(rows, true)
}

func (s *Store) scanEvents(rows *sql.Rows, reverse bool) ([]event.Event, error) {
	var out []event.Event
	for rows.Next() {
		var item event.Event
		var occurred string
		if err := rows.Scan(&item.Sequence, &item.EntityType, &item.EntityID, &item.Type, &occurred, &item.RequestID, &item.Actor, &item.Message, &item.ErrorCode); err != nil {
			return nil, s.storageError("scan event", err)
		}
		var err error
		item.OccurredAt, err = time.Parse(time.RFC3339Nano, occurred)
		if err != nil {
			return nil, s.storageError("parse event time", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, s.storageError("iterate events", err)
	}
	if reverse {
		for left, right := 0, len(out)-1; left < right; left, right = left+1, right-1 {
			out[left], out[right] = out[right], out[left]
		}
	}
	return out, nil
}
