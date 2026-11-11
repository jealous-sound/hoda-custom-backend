package traffic

import (
	"net/http"
	"testing"
	"time"
)

type clock struct{ current time.Time }

func (c *clock) now() time.Time { return c.current }

func (c *clock) advance(by time.Duration) { c.current = c.current.Add(by) }

func newClock() *clock {
	return &clock{current: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
}

func TestRequestsAreCountedByMinuteAndOutcome(t *testing.T) {
	c := newClock()
	recorder := NewRecorder(c.now)
	recorder.Request("GET /a", http.StatusOK, time.Millisecond)
	recorder.Request("GET /a", http.StatusUnprocessableEntity, time.Millisecond)
	recorder.Request("GET /a", http.StatusTooManyRequests, time.Millisecond)
	c.advance(time.Minute)
	recorder.Request("GET /a", http.StatusInternalServerError, time.Millisecond)
	minutes := recorder.Snapshot().Minutes
	if len(minutes) != 2 {
		t.Fatalf("minutes = %+v, want two", minutes)
	}
	first, second := minutes[0], minutes[1]
	if first.Requests != 3 || first.ClientErrors != 1 || first.Limited != 1 || first.ServerErrors != 0 {
		t.Fatalf("first minute = %+v", first)
	}
	if second.Requests != 1 || second.ServerErrors != 1 || second.Start-first.Start != 60 {
		t.Fatalf("second minute = %+v", second)
	}
}

func TestOnlyTheLastHourOfMinutesIsKept(t *testing.T) {
	c := newClock()
	recorder := NewRecorder(c.now)
	for range MinutesKept + 10 {
		recorder.Request("GET /a", http.StatusOK, time.Millisecond)
		c.advance(time.Minute)
	}
	if got := len(recorder.Snapshot().Minutes); got != MinutesKept {
		t.Fatalf("kept %d minutes, want %d", got, MinutesKept)
	}
}

func TestRoutesReportMeanAndSlowestBusiestFirst(t *testing.T) {
	c := newClock()
	recorder := NewRecorder(c.now)
	recorder.Request("GET /quiet", http.StatusOK, time.Millisecond)
	recorder.Request("GET /busy", http.StatusOK, 2*time.Millisecond)
	recorder.Request("GET /busy", http.StatusInternalServerError, 6*time.Millisecond)
	routes := recorder.Snapshot().Routes
	if len(routes) != 2 || routes[0].Route != "GET /busy" {
		t.Fatalf("routes = %+v, want the busy route first", routes)
	}
	busy := routes[0]
	if busy.Requests != 2 || busy.ServerErrors != 1 || busy.MeanMicros != 4000 || busy.MaxMicros != 6000 {
		t.Fatalf("busy route = %+v", busy)
	}
}

func TestRefusedActionsAreCountedAndListedNewestFirst(t *testing.T) {
	c := newClock()
	recorder := NewRecorder(c.now)
	recorder.Action("a", "battle", "")
	recorder.Action("a", "battle", "outcome_mismatch")
	c.advance(time.Second)
	recorder.Action("b", "purchase", "cant_afford")
	snapshot := recorder.Snapshot()
	if len(snapshot.Actions) != 2 || snapshot.Actions[0] != (Action{Action: "battle", Count: 2, Refused: 1}) {
		t.Fatalf("actions = %+v", snapshot.Actions)
	}
	if len(snapshot.RefusalCodes) != 2 {
		t.Fatalf("refusal codes = %+v", snapshot.RefusalCodes)
	}
	newest := Refusal{Time: c.current.Unix(), PlayerID: "b", Action: "purchase", Code: "cant_afford"}
	if len(snapshot.Refusals) != 2 || snapshot.Refusals[0] != newest {
		t.Fatalf("refusals = %+v, want the purchase first", snapshot.Refusals)
	}
}

func TestOnlyTheLatestRefusalsAreKept(t *testing.T) {
	c := newClock()
	recorder := NewRecorder(c.now)
	for range RefusalsKept + 10 {
		recorder.Action("a", "battle", "cant_afford")
	}
	snapshot := recorder.Snapshot()
	if len(snapshot.Refusals) != RefusalsKept || snapshot.Actions[0].Refused != RefusalsKept+10 {
		t.Fatalf("kept %d refusals of %d counted", len(snapshot.Refusals), snapshot.Actions[0].Refused)
	}
}
