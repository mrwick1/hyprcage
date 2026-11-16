package condition

import (
	"context"
	"testing"
	"time"
)

func TestWaitOutcomes(t *testing.T) {
	for _, status := range []string{"satisfied", "failed", "unsupported"} {
		t.Run(status, func(t *testing.T) {
			calls := 0
			result := Wait(context.Background(), time.Second, time.Millisecond, func(context.Context) Observation { calls++; return Observation{Status: status, Evidence: "observed"} })
			if result.Status != status || calls != 1 || result.Last.Evidence != "observed" {
				t.Fatal(result, calls)
			}
		})
	}
}
func TestWaitTimeoutAndCancellation(t *testing.T) {
	result := Wait(context.Background(), 10*time.Millisecond, time.Millisecond, func(context.Context) Observation { return Observation{Status: "pending", Evidence: "still running"} })
	if result.Status != "timed_out" || result.Last.Evidence != "still running" {
		t.Fatal(result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = Wait(ctx, time.Second, time.Millisecond, func(context.Context) Observation { t.Fatal("observed after cancellation"); return Observation{} })
	if result.Status != "cancelled" {
		t.Fatal(result)
	}
}
func TestWaitCancelsObservation(t *testing.T) {
	result := Wait(context.Background(), 10*time.Millisecond, time.Millisecond, func(ctx context.Context) Observation { <-ctx.Done(); return Observation{Status: "pending"} })
	if result.Status != "timed_out" {
		t.Fatal(result)
	}
}
