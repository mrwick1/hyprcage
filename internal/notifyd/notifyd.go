package notifyd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/godbus/dbus/v5"
)

const iface = "org.freedesktop.Notifications"

// rules are the BecomeMonitor match rules. Method returns carry no
// interface or member, so every return is seen, and observe keeps those
// that answer a Notify call.
var rules = []string{
	"type='method_call',interface='" + iface + "',member='Notify'",
	"type='method_return'",
	"type='signal',interface='" + iface + "',member='NotificationClosed'",
	"type='signal',interface='" + iface + "',member='ActionInvoked'",
}

// call identifies a method call on the bus: its sender and its serial.
type call struct {
	sender string
	serial uint32
}

// pairer holds the Notify calls that wait for their return.
type pairer map[call]Entry

// observe turns a monitored message into an entry. A Notify call gives
// none until its return (destination and reply serial equal to the call's
// sender and serial) brings the ID. Returns to other calls give none.
func (p pairer) observe(msg *dbus.Message, serial uint32, now time.Time) (Entry, bool) {
	str := func(f dbus.HeaderField) string { s, _ := msg.Headers[f].Value().(string); return s }
	switch msg.Type {
	case dbus.TypeMethodCall:
		var e Entry
		var replaces uint32
		var icon string
		var actions []string
		if str(dbus.FieldMember) != "Notify" || dbus.Store(msg.Body[:min(len(msg.Body), 6)], &e.App, &replaces, &icon, &e.Summary, &e.Body, &actions) != nil {
			return Entry{}, false
		}
		for i := 0; i+1 < len(actions); i += 2 {
			if e.Actions == nil {
				e.Actions = map[string]string{}
			}
			e.Actions[actions[i]] = actions[i+1]
		}
		e.Event, e.Time = "notify", now
		if len(p) >= 256 { // calls whose return never came, such as errors
			clear(p)
		}
		p[call{str(dbus.FieldSender), serial}] = e
	case dbus.TypeMethodReply:
		rs, _ := msg.Headers[dbus.FieldReplySerial].Value().(uint32)
		k := call{str(dbus.FieldDestination), rs}
		e, ok := p[k]
		if !ok || len(msg.Body) != 1 {
			return Entry{}, false
		}
		delete(p, k)
		if e.ID, ok = msg.Body[0].(uint32); ok {
			return e, true
		}
	case dbus.TypeSignal:
		if str(dbus.FieldInterface) != iface || len(msg.Body) != 2 {
			return Entry{}, false
		}
		id, _ := msg.Body[0].(uint32)
		switch str(dbus.FieldMember) {
		case "NotificationClosed":
			return Entry{ID: id, Event: "closed", Time: now}, true
		case "ActionInvoked":
			key, _ := msg.Body[1].(string)
			return Entry{ID: id, Event: "action", Action: key, Time: now}, true
		}
	}
	return Entry{}, false
}

// Run monitors the session bus and appends each notification event to
// Path() until ctx ends. It prunes the file on start and every hour.
func Run(ctx context.Context) error {
	path := Path()
	if err := Prune(path, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "notifyd: prune:", err)
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.Monitoring.BecomeMonitor", 0, rules, uint32(0)).Err; err != nil {
		return fmt.Errorf("BecomeMonitor: %w", err)
	}
	// godbus drops a message when this channel is full.
	msgs := make(chan *dbus.Message, 256)
	conn.Eavesdrop(msgs)
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	p := pairer{}
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-tick.C:
			if err := Prune(path, now); err != nil {
				fmt.Fprintln(os.Stderr, "notifyd: prune:", err)
			}
		case msg, ok := <-msgs:
			if !ok {
				return errors.New("the session bus closed the connection")
			}
			if e, ok := p.observe(msg, msg.Serial(), time.Now()); ok {
				if err := Append(path, e); err != nil {
					return err
				}
			}
		}
	}
}
