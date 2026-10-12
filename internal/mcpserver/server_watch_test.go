package mcpserver

import (
	"testing"
	"time"
)

func TestWatchParams(t *testing.T) {
	n, iv := watchParams(watchIn{})
	if n != 5 || iv != time.Second {
		t.Errorf("defaults: %d frames, %v", n, iv)
	}
	n, iv = watchParams(watchIn{Seconds: 99, FPS: 9})
	if n != 120 || iv != 250*time.Millisecond {
		t.Errorf("caps: %d frames, %v", n, iv)
	}
}
