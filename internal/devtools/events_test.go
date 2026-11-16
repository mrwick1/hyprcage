package devtools

import (
	"fmt"
	"github.com/hexadecimil/offstage/internal/cdp"
	"testing"
)

func TestCompletedEventsAndCursor(t *testing.T) {
	s, _ := newFake()
	cursor := s.Cursor()
	s.event(cdp.Event{SessionID: "p", Method: "Network.requestWillBeSent", Params: []byte(`{"requestId":"1","timestamp":1,"request":{"url":"https://test/submit","method":"POST"}}`)})
	s.event(cdp.Event{SessionID: "p", Method: "Network.responseReceived", Params: []byte(`{"requestId":"1","response":{"status":500}}`)})
	events, _, err := s.Events(cursor)
	if err != nil || len(events) != 0 {
		t.Fatal("response is not completed", events, err)
	}
	s.event(cdp.Event{SessionID: "p", Method: "Network.loadingFinished", Params: []byte(`{"requestId":"1","timestamp":2}`)})
	events, next, err := s.Events(cursor)
	if err != nil || len(events) != 1 || events[0].Request.Status != 500 {
		t.Fatal(events, err)
	}
	events, _, err = s.Events(next)
	if err != nil || len(events) != 0 {
		t.Fatal("old event repeated")
	}
	foreign, _ := newFake()
	if _, _, err = foreign.Events(cursor); err == nil {
		t.Fatal("foreign collector accepted")
	}
	for i := 0; i < MaxNetwork+1; i++ {
		s.mu.Lock()
		s.complete(&Req{URL: "https://test/"})
		s.mu.Unlock()
	}
	if _, _, err = s.Events(cursor); err == nil {
		t.Fatal("expired cursor accepted")
	}

}

func TestCursorDetectsLostRequests(t *testing.T) {
	s, _ := newFake()
	cursor := s.Cursor()
	s.mu.Lock()
	for i := 0; i < MaxNetwork+1; i++ {
		s.track(fmt.Sprint(i), &Req{URL: "https://test/"})
	}
	s.mu.Unlock()
	if _, _, err := s.Events(cursor); err == nil {
		t.Fatal("lost in-flight requests accepted")
	}
	cursor = s.Cursor()
	s.Network(false, "", true)
	if _, _, err := s.Events(cursor); err == nil {
		t.Fatal("cleared in-flight requests accepted")
	}
}

func TestConsoleEvidenceRetainsClearLoss(t *testing.T) {
	s, _ := newFake()
	s.add("p", Msg{Text: "one"})
	s.Console(true)
	messages, lost := s.ConsoleEvidence()
	if len(messages) != 0 || lost != 1 {
		t.Fatal(messages, lost)
	}
	s.add("p", Msg{Text: "two"})
	messages, lost = s.ConsoleEvidence()
	if len(messages) != 1 || messages[0].Sequence != 2 || lost != 1 {
		t.Fatal(messages, lost)
	}
}
