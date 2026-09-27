// Command outboxctl inspects and requeues parked order events.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type event struct {
	ID          string     `json:"id"`
	AggregateID string     `json:"aggregate_id"`
	EventType   string     `json:"event_type"`
	RoutingKey  string     `json:"routing_key"`
	Attempts    int32      `json:"attempts"`
	LastError   string     `json:"last_error"`
	FailedAt    *time.Time `json:"failed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func main() {
	command := flag.String("command", "list", "list, retry, or purge-published")
	dsn := flag.String("dsn", os.Getenv("EAGLE_DATABASE_DSN"), "order PostgreSQL DSN")
	id := flag.String("id", "", "parked event ID for retry")
	olderThan := flag.Duration("older-than", 7*24*time.Hour, "published-event retention used by purge-published")
	yes := flag.Bool("yes", false, "confirm a state-changing command")
	flag.Parse()
	if *dsn == "" {
		panic("outboxctl: -dsn or EAGLE_DATABASE_DSN is required")
	}
	db, err := sql.Open("pgx", *dsn)
	if err != nil {
		panic(err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		panic(err)
	}
	switch *command {
	case "list":
		err = list(ctx, db)
	case "retry":
		if !*yes || *id == "" {
			err = fmt.Errorf("retry requires -id and -yes")
		} else {
			err = retry(ctx, db, *id)
		}
	case "purge-published":
		if !*yes || *olderThan <= 0 {
			err = fmt.Errorf("purge-published requires positive -older-than and -yes")
		} else {
			err = purgePublished(ctx, db, *olderThan)
		}
	default:
		err = fmt.Errorf("unsupported command %q", *command)
	}
	if err != nil {
		panic(err)
	}
}

func list(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
SELECT id, aggregate_id, event_type, routing_key, attempts, last_error, failed_at, created_at
FROM event_outbox
WHERE failed_at IS NOT NULL
ORDER BY failed_at, created_at
LIMIT 200`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	encoder := json.NewEncoder(os.Stdout)
	for rows.Next() {
		var value event
		if err := rows.Scan(&value.ID, &value.AggregateID, &value.EventType, &value.RoutingKey,
			&value.Attempts, &value.LastError, &value.FailedAt, &value.CreatedAt); err != nil {
			return err
		}
		if err := encoder.Encode(value); err != nil {
			return err
		}
	}
	return rows.Err()
}

func retry(ctx context.Context, db *sql.DB, id string) error {
	result, err := db.ExecContext(ctx, `
UPDATE event_outbox
SET failed_at = NULL, locked_until = NULL, available_at = now(), attempts = 0, last_error = ''
WHERE id = $1 AND failed_at IS NOT NULL`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("parked outbox event %q not found", id)
	}
	fmt.Printf("requeued %s\n", id)
	return nil
}

func purgePublished(ctx context.Context, db *sql.DB, olderThan time.Duration) error {
	result, err := db.ExecContext(ctx, `
DELETE FROM event_outbox
WHERE published_at < now() - ($1 * interval '1 second')`, int64(olderThan/time.Second))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	fmt.Printf("deleted %d published events\n", count)
	return nil
}
