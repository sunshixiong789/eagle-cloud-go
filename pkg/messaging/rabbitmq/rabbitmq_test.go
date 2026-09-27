package rabbitmq

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHandleWithRetryStopsAfterSuccess(t *testing.T) {
	calls, retries := 0, 0
	err := handleWithRetry(context.Background(), 5, time.Nanosecond, Message{}, func(context.Context, Message) error {
		calls++
		if calls < 3 {
			return errors.New("temporary")
		}
		return nil
	}, func() { retries++ })
	if err != nil || calls != 3 || retries != 2 {
		t.Fatalf("result err=%v calls=%d retries=%d", err, calls, retries)
	}
}

func TestHandleWithRetryDoesNotRetryPermanentFailure(t *testing.T) {
	calls := 0
	err := handleWithRetry(context.Background(), 5, time.Nanosecond, Message{}, func(context.Context, Message) error {
		calls++
		return Permanent(errors.New("malformed"))
	}, nil)
	if !IsPermanent(err) || calls != 1 {
		t.Fatalf("result err=%v calls=%d", err, calls)
	}
}

func TestHandleWithRetryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := handleWithRetry(ctx, 5, time.Hour, Message{}, func(context.Context, Message) error {
		calls++
		cancel()
		return errors.New("temporary")
	}, nil)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("result err=%v calls=%d", err, calls)
	}
}
