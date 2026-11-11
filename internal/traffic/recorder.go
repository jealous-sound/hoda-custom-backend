package traffic

import (
	"cmp"
	"net/http"
	"slices"
	"sync"
	"time"
)

const (
	MinutesKept  = 60
	RefusalsKept = 200
)

type Minute struct {
	Start        int64 `json:"start"`
	Requests     int   `json:"requests"`
	ClientErrors int   `json:"client_errors"`
	ServerErrors int   `json:"server_errors"`
	Limited      int   `json:"limited"`
}

type Route struct {
	Route        string `json:"route"`
	Requests     int64  `json:"requests"`
	ClientErrors int64  `json:"client_errors"`
	ServerErrors int64  `json:"server_errors"`
	Limited      int64  `json:"limited"`
	MeanMicros   int64  `json:"mean_micros"`
	MaxMicros    int64  `json:"max_micros"`
}

type Action struct {
	Action  string `json:"action"`
	Count   int64  `json:"count"`
	Refused int64  `json:"refused"`
}

type RefusalCode struct {
	Code  string `json:"code"`
	Count int64  `json:"count"`
}

type Refusal struct {
	Time     int64  `json:"time"`
	PlayerID string `json:"player_id"`
	Action   string `json:"action"`
	Code     string `json:"code"`
}

type Snapshot struct {
	Since        int64         `json:"since"`
	Minutes      []Minute      `json:"minutes"`
	Routes       []Route       `json:"routes"`
	Actions      []Action      `json:"actions"`
	RefusalCodes []RefusalCode `json:"refusal_codes"`
	Refusals     []Refusal     `json:"refusals"`
}

type routeTotals struct {
	Route
	total time.Duration
}

type Recorder struct {
	mutex    sync.Mutex
	now      func() time.Time
	since    time.Time
	minutes  []Minute
	routes   map[string]*routeTotals
	actions  map[string]*Action
	codes    map[string]int64
	refusals []Refusal
}

func NewRecorder(now func() time.Time) *Recorder {
	return &Recorder{
		now: now, since: now(), routes: map[string]*routeTotals{}, actions: map[string]*Action{},
		codes: map[string]int64{},
	}
}

func (r *Recorder) Request(route string, status int, took time.Duration) {
	now := r.now()
	limited := status == http.StatusTooManyRequests
	clientError := status >= 400 && status < 500 && !limited
	serverError := status >= 500
	r.mutex.Lock()
	defer r.mutex.Unlock()
	minute := r.minuteLocked(now)
	minute.Requests++
	totals, known := r.routes[route]
	if !known {
		totals = &routeTotals{Route: Route{Route: route}}
		r.routes[route] = totals
	}
	totals.Requests++
	totals.total += took
	totals.MaxMicros = max(totals.MaxMicros, took.Microseconds())
	switch {
	case limited:
		minute.Limited++
		totals.Limited++
	case clientError:
		minute.ClientErrors++
		totals.ClientErrors++
	case serverError:
		minute.ServerErrors++
		totals.ServerErrors++
	}
}

func (r *Recorder) Action(playerID, action, refusalCode string) {
	now := r.now()
	r.mutex.Lock()
	defer r.mutex.Unlock()
	counted, known := r.actions[action]
	if !known {
		counted = &Action{Action: action}
		r.actions[action] = counted
	}
	counted.Count++
	if refusalCode == "" {
		return
	}
	counted.Refused++
	r.codes[refusalCode]++
	r.refusals = append(r.refusals, Refusal{Time: now.Unix(), PlayerID: playerID, Action: action, Code: refusalCode})
	if extra := len(r.refusals) - RefusalsKept; extra > 0 {
		r.refusals = slices.Clone(r.refusals[extra:])
	}
}

func (r *Recorder) minuteLocked(now time.Time) *Minute {
	start := now.Truncate(time.Minute).Unix()
	if last := len(r.minutes) - 1; last < 0 || r.minutes[last].Start != start {
		r.minutes = append(r.minutes, Minute{Start: start})
		if extra := len(r.minutes) - MinutesKept; extra > 0 {
			r.minutes = slices.Clone(r.minutes[extra:])
		}
	}
	return &r.minutes[len(r.minutes)-1]
}

func (r *Recorder) Snapshot() Snapshot {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	snapshot := Snapshot{
		Since: r.since.Unix(), Minutes: append([]Minute{}, r.minutes...), Routes: make([]Route, 0, len(r.routes)),
		Actions: make([]Action, 0, len(r.actions)), RefusalCodes: make([]RefusalCode, 0, len(r.codes)),
		Refusals: append([]Refusal{}, r.refusals...),
	}
	slices.Reverse(snapshot.Refusals)
	for _, totals := range r.routes {
		route := totals.Route
		route.MeanMicros = totals.total.Microseconds() / route.Requests
		snapshot.Routes = append(snapshot.Routes, route)
	}
	slices.SortFunc(snapshot.Routes, func(a, b Route) int {
		return cmp.Or(cmp.Compare(b.Requests, a.Requests), cmp.Compare(a.Route, b.Route))
	})
	for _, action := range r.actions {
		snapshot.Actions = append(snapshot.Actions, *action)
	}
	slices.SortFunc(snapshot.Actions, func(a, b Action) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Action, b.Action))
	})
	for code, count := range r.codes {
		snapshot.RefusalCodes = append(snapshot.RefusalCodes, RefusalCode{Code: code, Count: count})
	}
	slices.SortFunc(snapshot.RefusalCodes, func(a, b RefusalCode) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Code, b.Code))
	})
	return snapshot
}
