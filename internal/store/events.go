package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
)

func appendEventTx(ctx context.Context, tx *sql.Tx, value event.Event) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO events(entity_type,entity_id,event_type,occurred_at,request_id,message,error_code)
		VALUES(?,?,?,?,?,?,?)`, value.EntityType, value.EntityID, value.Type, value.OccurredAt.UTC().Format(time.RFC3339Nano), value.RequestID, value.Message, value.ErrorCode)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *Store) Events(ctx context.Context, entityType, entityID string) ([]event.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sequence,entity_type,entity_id,event_type,occurred_at,request_id,message,error_code
		FROM events WHERE entity_type=? AND entity_id=? ORDER BY sequence`, entityType, entityID)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var out []event.Event
	for rows.Next() {
		var item event.Event
		var occurred string
		if err := rows.Scan(&item.Sequence, &item.EntityType, &item.EntityID, &item.Type, &occurred, &item.RequestID, &item.Message, &item.ErrorCode); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		item.OccurredAt, err = time.Parse(time.RFC3339Nano, occurred)
		if err != nil {
			return nil, fmt.Errorf("parse event time: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
