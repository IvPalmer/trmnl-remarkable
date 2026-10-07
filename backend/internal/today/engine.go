package today

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"reflect"
	"sync"
	"time"
)

const (
	MsgToday     uint32 = 109
	MsgActResult uint32 = 110
)

// Emitter sends one message to the QML. The engine calls it with its lock
// held, so messages leave in the order the state changed; it must therefore
// never call back into the engine.
type Emitter func(typ uint32, payload string)

// Snapshot is message 109.
type Snapshot struct {
	Configured bool      `json:"configured"`
	Problem    string    `json:"problem,omitempty"`
	Refreshing bool      `json:"refreshing"`
	Sections   []Section `json:"sections"`
}

type actResult struct {
	OK      bool   `json:"ok"`
	Section string `json:"section"`
	Action  string `json:"action"`
	Key     string `json:"key"`
	Message string `json:"message"`
}

// NotConfigured is message 109 when today.json is missing or wrong.
func NotConfigured(problem string) string {
	return mustJSON(Snapshot{Configured: false, Problem: problem, Sections: []Section{}})
}

type sectionState struct {
	src       Source
	raw       json.RawMessage
	built     Built
	fetchedAt time.Time
	rev       uint64
	actions   uint64 // finished actions: a fetch begun before the latest is stale
	err       string
}

// Engine owns Today's state and its rules (spec, "Actions"):
//   - one fetch at a time;
//   - one action at a time;
//   - an action names the rev it was drawn from;
//   - a fetch that started before an action finished is dropped;
//   - refetch requests are a set, fetched once after the running fetch;
//   - a section's rev changes whenever its model does; a fetch or rebuild
//     that changes nothing keeps it;
//   - nothing begun before a Clear cache is committed after it.
//
// Network calls run without the lock; everything else holds it.
type Engine struct {
	ctx       context.Context
	client    Client
	loc       *time.Location
	now       func() time.Time
	cachePath string
	emit      Emitter

	mu       sync.Mutex
	order    []string
	sections map[string]*sectionState // never changes after New
	revSeq   uint64
	fetching bool
	acting   bool
	pending  map[string]bool
	clears   uint64 // ClearCache calls: work begun before the latest is dropped
}

func New(ctx context.Context, client Client, sources []Source, loc *time.Location,
	cachePath string, emit Emitter, now func() time.Time) *Engine {
	e := &Engine{ctx: ctx, client: client, loc: loc, now: now, cachePath: cachePath, emit: emit,
		sections: map[string]*sectionState{}, pending: map[string]bool{},
		// Revs start from the clock, so a sheet drawn before a restart never
		// matches one drawn after it.
		revSeq: uint64(now().UnixMilli())}
	cached := loadCache(cachePath)
	for _, s := range sources {
		st := &sectionState{src: s, rev: e.nextRev()}
		if c, ok := cached[s.ID()]; ok {
			if b, err := s.Build(c.Raw, now(), loc); err == nil {
				st.raw, st.built, st.fetchedAt = c.Raw, b, c.FetchedAt
			}
		}
		e.order = append(e.order, s.ID())
		e.sections[s.ID()] = st
	}
	return e
}

func (e *Engine) nextRev() uint64 { e.revSeq++; return e.revSeq }

// Refresh sends what is known now, then fetches every section.
func (e *Engine) Refresh() { e.fetch(e.order, false) }

// Current is what message 109 would carry now.
func (e *Engine) Current() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked()
}

// fetch rebuilds what is cached as of now, then starts a fetch of ids. If one
// is running, a required fetch (a refetch an action asked for) waits in the
// pending set; a Refresh is ignored.
func (e *Engine) fetch(ids []string, required bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rebuildLocked()
	if e.fetching {
		if required {
			for _, id := range ids {
				e.pending[id] = true
			}
		}
		e.emitSnapshotLocked()
		return
	}
	e.fetching = true
	started := map[string]uint64{}
	for _, id := range ids {
		started[id] = e.sections[id].actions
	}
	e.emitSnapshotLocked()
	go e.runFetch(ids, started, e.clears)
}

func (e *Engine) runFetch(ids []string, started map[string]uint64, clears uint64) {
	var wg sync.WaitGroup
	for _, id := range ids {
		st := e.sections[id]
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, err := st.src.Fetch(e.ctx, e.client)
			var b Built
			if err == nil {
				b, err = st.src.Build(raw, e.now(), e.loc)
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			switch {
			case e.clears != clears:
				return // the cache was cleared meanwhile; nothing comes back
			case st.actions != started[id]:
				return // an action finished meanwhile; its data is newer
			case err != nil:
				// Build failed too, or Fetch did: the last good data stays,
				// in the model and in the cache.
				st.err = failure(err, st.raw != nil)
				return
			}
			// Opening Today refreshes it: a refresh that changes nothing
			// keeps the rev, so the sheet on screen can still act.
			if st.raw == nil || !reflect.DeepEqual(b, st.built) {
				st.rev = e.nextRev()
			}
			st.raw, st.built, st.fetchedAt, st.err = raw, b, e.now(), ""
			e.saveLocked()
		}()
	}
	wg.Wait()
	e.mu.Lock()
	e.fetching = false
	next := e.takePendingLocked()
	e.emitSnapshotLocked()
	e.mu.Unlock()
	if len(next) > 0 {
		e.fetch(next, true)
	}
}

// Act runs one action and answers with message 110, then a fresh 109.
func (e *Engine) Act(sectionID string, rev uint64, action, key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	reply := func(msg string) {
		e.emit(MsgActResult, mustJSON(actResult{OK: false, Section: sectionID, Action: action, Key: key, Message: msg}))
	}
	st, ok := e.sections[sectionID]
	switch {
	case !ok:
		reply("unknown section")
		return
	case e.acting:
		reply("another action is in progress")
		return
	case st.rev != rev:
		reply("this list changed; check it again")
		return
	}
	ref, ok := st.built.Refs[key]
	if !ok {
		reply("this item is no longer there")
		return
	}
	e.acting = true
	go e.runAct(st, sectionID, action, key, ref, st.raw, e.clears)
}

func (e *Engine) runAct(st *sectionState, sectionID, action, key string, ref ItemRef, raw json.RawMessage, clears uint64) {
	res, err := st.src.Act(e.ctx, e.client, action, ref, raw)
	var b Built
	var berr error
	if err == nil && res.Raw != nil {
		b, berr = st.src.Build(res.Raw, e.now(), e.loc)
	}
	e.mu.Lock()
	e.acting = false
	st.actions++
	// After a Clear cache, what the action returned (merged into the data it
	// started from) and the refetch it asked for are both dropped.
	if e.clears == clears {
		if err == nil && res.Raw != nil && berr == nil {
			st.raw, st.built, st.fetchedAt, st.err = res.Raw, b, e.now(), ""
			st.rev = e.nextRev()
			e.saveLocked()
		}
		if res.Refetch || berr != nil {
			e.pending[sectionID] = true
		}
	}
	var next []string
	if !e.fetching {
		next = e.takePendingLocked()
	}
	msg := res.Message
	switch {
	case err != nil:
		msg = UserMessage(err)
	case msg == "":
		msg = "Done"
	}
	e.emit(MsgActResult, mustJSON(actResult{OK: err == nil, Section: sectionID, Action: action, Key: key, Message: msg}))
	e.emitSnapshotLocked()
	e.mu.Unlock()
	if len(next) > 0 {
		e.fetch(next, true)
	}
}

// ClearCache forgets every section's data, on disk too. A fetch or action
// already running, and a refetch already queued, can no longer bring it back.
func (e *Engine) ClearCache() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.clears++
	e.pending = map[string]bool{}
	for _, st := range e.sections {
		st.raw, st.built, st.fetchedAt, st.err = nil, Built{}, time.Time{}, ""
		st.rev = e.nextRev()
	}
	if err := os.Remove(e.cachePath); err != nil && !os.IsNotExist(err) {
		log.Printf("today: cache not removed: %v", err)
	}
	e.emitSnapshotLocked()
}

// rebuildLocked rebuilds every section that has data as of now, so the view
// left open across midnight regroups by the new date even when no fetch
// succeeds. A section whose model changed in any way gets a new rev: due's
// keys are list positions, so a moved key must make every open sheet stale.
func (e *Engine) rebuildLocked() {
	now := e.now()
	for _, id := range e.order {
		st := e.sections[id]
		if st.raw == nil {
			continue
		}
		// This raw built before; if it no longer does, keep the model it gave.
		b, err := st.src.Build(st.raw, now, e.loc)
		if err != nil || reflect.DeepEqual(b, st.built) {
			continue
		}
		st.built = b
		st.rev = e.nextRev()
	}
}

// Texts from the spec's error-handling table.
const (
	cantReach   = "Can't reach the Mac" // UserMessage's text for an unreachable Mac
	offline     = "offline"             // beside "as of HH:MM" in the view
	needsTheMac = "Can't reach the Mac. Today needs the Mac awake."
)

// failure is what a section says when its fetch fails. An unreachable Mac
// is "offline" beside cached data, and a plea to wake the Mac without any.
// The tablet's own problems (Tailscale down, its grant refused) are said as
// they are. Anything else is this source failing on its own: "unavailable",
// with the error. cached is fixed for as long as the text is shown: data
// only arrives with a success, which clears the error, and ClearCache
// clears both.
func failure(err error, cached bool) string {
	msg := UserMessage(err)
	var he *HTTPError
	switch {
	case msg == cantReach && cached:
		return offline
	case msg == cantReach:
		return needsTheMac
	case errors.Is(err, ErrTailscaleDown),
		errors.As(err, &he) && (he.Status == http.StatusUnauthorized || he.Status == http.StatusForbidden):
		return msg
	default:
		return "unavailable: " + msg
	}
}

func (e *Engine) takePendingLocked() []string {
	var ids []string
	for _, id := range e.order {
		if e.pending[id] {
			ids = append(ids, id)
		}
	}
	e.pending = map[string]bool{}
	return ids
}

func (e *Engine) saveLocked() {
	out := map[string]cachedSection{}
	for id, st := range e.sections {
		if st.raw != nil {
			out[id] = cachedSection{Raw: st.raw, FetchedAt: st.fetchedAt}
		}
	}
	if err := saveCache(e.cachePath, out); err != nil {
		log.Printf("today: cache not saved: %v", err)
	}
}

// asOf words when data was fetched: the time alone for today's data, and
// with the day for anything older, so a cache from last night never reads
// as tonight's.
func (e *Engine) asOf(fetchedAt time.Time) string {
	at, now := fetchedAt.In(e.loc), e.now().In(e.loc)
	if at.Format(dateLayout) == now.Format(dateLayout) {
		return at.Format("15:04")
	}
	return at.Format("Mon 2 Jan 15:04")
}

func (e *Engine) snapshotLocked() Snapshot {
	s := Snapshot{Configured: true, Refreshing: e.fetching, Sections: []Section{}}
	for _, id := range e.order {
		st := e.sections[id]
		sec := Section{ID: id, Rev: st.rev, Title: st.built.Title, Placement: st.src.Placement(),
			Status: "none", Warning: st.built.Warning, Error: st.err, Groups: st.built.Groups}
		if sec.Title == "" {
			sec.Title = st.src.Title() // most data brings no heading of its own
		}
		if sec.Groups == nil {
			sec.Groups = []Group{}
		}
		switch {
		case st.err != "":
			sec.Status = "error"
		case st.raw != nil:
			sec.Status = "ok"
		}
		if !st.fetchedAt.IsZero() {
			sec.AsOf = e.asOf(st.fetchedAt)
		}
		s.Sections = append(s.Sections, sec)
	}
	return s
}

func (e *Engine) emitSnapshotLocked() { e.emit(MsgToday, mustJSON(e.snapshotLocked())) }

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
