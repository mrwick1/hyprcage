package notifyd

import (
	"regexp"
	"slices"
	"time"
)

// Notification is the state of one ID after its events: the last notify
// (a replacement overwrites it), the last invoked action and the close.
type Notification struct {
	Entry
	Closed bool `json:"closed"`
}

// List folds the events into notifications, newest first, filtered by since,
// app (exact) and match (regexp on the summary and body). Repeated closed
// events for one ID, as swaync sends them, fold into one.
func List(entries []Entry, since time.Time, app string, match *regexp.Regexp) []Notification {
	byID := map[uint32]*Notification{}
	var all []*Notification
	for _, e := range entries {
		n := byID[e.ID]
		switch {
		case e.Event == "notify":
			n = &Notification{Entry: e}
			byID[e.ID] = n
			all = append(all, n)
		case n == nil: // its notify was pruned
		case e.Event == "closed":
			n.Closed = true
		case e.Event == "action":
			n.Action = e.Action
		}
	}
	var out []Notification
	for _, n := range slices.Backward(all) {
		if byID[n.ID] != n || n.Time.Before(since) || app != "" && n.App != app ||
			match != nil && !match.MatchString(n.Summary+"\n"+n.Body) {
			continue
		}
		out = append(out, *n)
	}
	return out
}

// Latest returns the newest notification that is not closed.
func Latest(entries []Entry) (Notification, bool) {
	for _, n := range List(entries, time.Time{}, "", nil) {
		if !n.Closed {
			return n, true
		}
	}
	return Notification{}, false
}
