package notifyd

import (
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestPairNotifyReply(t *testing.T) {
	call := &dbus.Message{
		Type: dbus.TypeMethodCall,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldSender:    dbus.MakeVariant(":1.5"),
			dbus.FieldInterface: dbus.MakeVariant("org.freedesktop.Notifications"),
			dbus.FieldMember:    dbus.MakeVariant("Notify"),
		},
		Body: []any{"app", uint32(0), "", "hi", "there", []string{"ok", "OK"}, map[string]dbus.Variant{}, int32(-1)},
	}
	reply := &dbus.Message{
		Type: dbus.TypeMethodReply,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldDestination: dbus.MakeVariant(":1.5"),
			dbus.FieldReplySerial: dbus.MakeVariant(uint32(7)),
		},
		Body: []any{uint32(42)},
	}
	p := pairer{}
	now := time.Now()
	if _, ok := p.observe(call, 7, now); ok {
		t.Fatal("a call alone gave an entry")
	}
	e, ok := p.observe(reply, 3, now)
	if !ok {
		t.Fatal("the reply gave no entry")
	}
	if e.ID != 42 || e.Event != "notify" || e.App != "app" || e.Summary != "hi" || e.Body != "there" || e.Actions["ok"] != "OK" {
		t.Fatalf("got %+v", e)
	}
	if _, ok := p.observe(reply, 4, now); ok {
		t.Fatal("a second reply to the same call gave an entry")
	}
}
