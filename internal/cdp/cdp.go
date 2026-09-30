// Package cdp is a Chrome DevTools protocol client on the browser
// websocket, with flattened sessions.
package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Error is an "error" reply: the browser answered, the command failed.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("cdp error %d: %s", e.Code, e.Message) }

// Event is a message without an id: a notification from the browser.
type Event struct {
	Method    string          `json:"method"`
	SessionID string          `json:"sessionId"`
	Params    json.RawMessage `json:"params"`
}

// Conn sends the commands of every flattened session over the browser
// websocket and matches the responses by id.
type Conn struct {
	conn    *websocket.Conn
	onEvent func(Event)
	mu      sync.Mutex
	next    int64
	pending map[int64]chan reply
	done    chan struct{}
	err     error // set before done closes
}

type reply struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

// DefaultTimeout bounds one Call.
const DefaultTimeout = 5 * time.Second

// Dial connects to the browser websocket that /json/version names on
// 127.0.0.1:port. onEvent, when not nil, runs on the read loop for every
// event: it must not block and must not Call.
func Dial(ctx context.Context, port int, onEvent func(Event)) (*Conn, error) {
	hc := http.Client{Timeout: 5 * time.Second}
	resp, err := hc.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
	if err != nil {
		return nil, fmt.Errorf("no DevTools answer on 127.0.0.1:%d: %v", port, err)
	}
	defer resp.Body.Close()
	var v struct {
		URL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil || v.URL == "" {
		return nil, fmt.Errorf("no webSocketDebuggerUrl on 127.0.0.1:%d", port)
	}
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dctx, v.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %v", v.URL, err)
	}
	conn.SetReadLimit(-1) // a full AX tree is megabytes
	c := &Conn{conn: conn, onEvent: onEvent, pending: map[int64]chan reply{}, done: make(chan struct{})}
	go c.readLoop()
	return c, nil
}

func (c *Conn) readLoop() {
	for {
		_, data, err := c.conn.Read(context.Background())
		if err != nil {
			c.err = err
			close(c.done)
			return
		}
		var r reply
		if json.Unmarshal(data, &r) != nil {
			continue
		}
		if r.ID == 0 {
			var ev Event
			if c.onEvent != nil && json.Unmarshal(data, &ev) == nil && ev.Method != "" {
				c.onEvent(ev)
			}
			continue
		}
		c.mu.Lock()
		ch := c.pending[r.ID]
		delete(c.pending, r.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- r
		}
	}
}

// Call sends one command on sessionID ("" is the browser) and waits up to
// DefaultTimeout for its result.
func (c *Conn) Call(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	return c.CallFor(ctx, DefaultTimeout, sessionID, method, params)
}

// CallFor is Call with its own timeout, for commands that run long: a
// heap snapshot, a trace stream.
func (c *Conn) CallFor(ctx context.Context, timeout time.Duration, sessionID, method string, params any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ch := make(chan reply, 1)
	c.mu.Lock()
	c.next++
	id := c.next
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	msg, err := json.Marshal(struct {
		ID        int64  `json:"id"`
		Method    string `json:"method"`
		Params    any    `json:"params,omitempty"`
		SessionID string `json:"sessionId,omitempty"`
	}{id, method, params, sessionID})
	if err != nil {
		return nil, err
	}
	if err := c.conn.Write(ctx, websocket.MessageText, msg); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Error != nil {
			return nil, fmt.Errorf("%s: %w", method, r.Error)
		}
		return r.Result, nil
	case <-c.done:
		return nil, c.err
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

// Done closes when the connection is gone.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Close drops the connection.
func (c *Conn) Close() error { return c.conn.CloseNow() }
