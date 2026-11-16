package headless

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	"sync"
	"time"

	"github.com/hexadecimil/offstage/internal/cdp"
)

// Screencast gives the latest frame of the page that Chrome pushes with
// Page.startScreencast. Chrome sends a frame only when the page repaints, so
// Capture on a still page returns the same image again. It satisfies
// record.Capturer.
type Screencast struct {
	b       *Browser
	session string

	mu      sync.Mutex
	raw     []byte      // the latest JPEG frame, not yet decoded
	img     *image.RGBA // the latest frame, decoded
	stopped bool
}

// StartScreencast starts the screencast of the driven page at the page
// size. Only one runs at a time per Browser.
func (b *Browser) StartScreencast() (*Screencast, error) {
	b.mu.Lock()
	c := &Screencast{b: b, session: b.session}
	b.mu.Unlock()
	if !b.cast.CompareAndSwap(nil, c) {
		return nil, errors.New("headless: a screencast already runs")
	}
	if _, err := b.conn.Call(context.Background(), c.session, "Page.startScreencast", map[string]any{
		"format": "jpeg", "quality": 90, "maxWidth": b.width, "maxHeight": b.height, "everyNthFrame": 1}); err != nil {
		b.cast.Store(nil)
		return nil, err
	}
	return c, nil
}

// onFrame runs on the read loop. It keeps the frame and acknowledges it in
// another goroutine, because the read loop must not Call.
func (c *Screencast) onFrame(e cdp.Event) {
	var f struct {
		Data      string
		SessionID int
	}
	if json.Unmarshal(e.Params, &f) != nil || e.SessionID != c.session {
		return
	}
	if raw, err := base64.StdEncoding.DecodeString(f.Data); err == nil {
		c.mu.Lock()
		c.raw = raw
		c.mu.Unlock()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = c.b.conn.Call(ctx, c.session, "Page.screencastFrameAck", map[string]any{"sessionId": f.SessionID})
	}()
}

// Capture returns the latest frame. Until the first frame has arrived it
// takes a screenshot. cursor is ignored: the browser has no pointer.
func (c *Screencast) Capture(cursor bool) (*image.RGBA, error) {
	if !c.b.Alive() {
		return nil, errors.New("headless: Chrome is gone")
	}
	c.mu.Lock()
	if c.raw != nil {
		img, err := jpeg.Decode(bytes.NewReader(c.raw))
		if err != nil {
			c.mu.Unlock()
			return nil, err
		}
		c.raw = nil
		c.img = image.NewRGBA(img.Bounds())
		draw.Draw(c.img, c.img.Bounds(), img, img.Bounds().Min, draw.Src)
	}
	img := c.img
	c.mu.Unlock()
	if img == nil { // not under mu: onFrame needs it, and the read loop must go on
		return c.b.Capture(cursor)
	}
	return img, nil
}

// Stop ends the screencast.
func (c *Screencast) Stop() {
	c.mu.Lock()
	done := c.stopped
	c.stopped = true
	c.mu.Unlock()
	if done {
		return
	}
	c.b.cast.CompareAndSwap(c, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = c.b.conn.Call(ctx, c.session, "Page.stopScreencast", nil)
}
