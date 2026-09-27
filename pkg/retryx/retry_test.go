package retryx

import (
	"context"
	"errors"
	"testing"
)

func TestDoRetriesOnlyRetryableFailures(t *testing.T) {
	want := errors.New("temporary")
	calls := 0
	err := Do(context.Background(), 3, 0, func(err error) bool { return errors.Is(err, want) }, func() error {
		calls++
		if calls < 3 {
			return want
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("Do() = %v after %d calls", err, calls)
	}
}
