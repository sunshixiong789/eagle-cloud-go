// Package retryx provides bounded retries for explicitly idempotent operations.
package retryx

import (
	"context"
	"time"
)

func Do(ctx context.Context, attempts int, backoff time.Duration, retryable func(error) bool, call func() error) error {
	if attempts < 1 {
		attempts = 1
	}
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		err = call()
		if err == nil || attempt == attempts || !retryable(err) {
			return err
		}
		timer := time.NewTimer(backoff * time.Duration(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}
