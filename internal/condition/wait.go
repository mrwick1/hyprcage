// Package condition waits for observable outcomes with one deadline.
package condition

import (
	"context"
	"time"
)

type Observation struct {
	Status   string `json:"status"` // pending, satisfied, failed, unsupported
	Evidence any    `json:"evidence,omitempty"`
}
type Result struct {
	Status    string      `json:"status"`
	ElapsedMs int64       `json:"elapsed_ms"`
	Last      Observation `json:"last_observation"`
}

// Wait calls observe immediately, then polls until an outcome or deadline.
// The observer must honor its context and must not retain a shared UI lock.
func Wait(parent context.Context, timeout, interval time.Duration, observe func(context.Context) Observation) Result {
	start := time.Now()
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	var last Observation
	finish := func(status string) Result {
		return Result{Status: status, ElapsedMs: time.Since(start).Milliseconds(), Last: last}
	}
	interrupted := func() string {
		if parent.Err() != nil {
			return "cancelled"
		}
		return "timed_out"
	}
	for {
		if ctx.Err() != nil {
			return finish(interrupted())
		}
		last = observe(ctx)
		if ctx.Err() != nil {
			return finish(interrupted())
		}
		switch last.Status {
		case "satisfied", "failed", "unsupported":
			return finish(last.Status)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return finish(interrupted())
		case <-timer.C:
		}
	}
}
