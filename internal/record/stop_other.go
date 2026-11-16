//go:build !linux

package record

import (
	"context"
	"os"
	"time"
)

// The system has no signal that ends the recorder softly, and a kill would
// skip the ffmpeg finalisation. A stop file next to the state file takes
// its place.
func stopPath(target string) string { return StatePath(target) + ".stop" }

// requestStop creates the stop file that watchStop looks for.
func requestStop(s State) { _ = os.WriteFile(stopPath(s.Target), nil, 0o600) }

// watchStop returns a context that ends when the stop file appears.
func watchStop(ctx context.Context, target string) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	_ = os.Remove(stopPath(target)) // a request for an earlier recorder
	go func() {
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
				if _, err := os.Stat(stopPath(target)); err == nil {
					_ = os.Remove(stopPath(target))
					return
				}
			}
		}
	}()
	return ctx
}
