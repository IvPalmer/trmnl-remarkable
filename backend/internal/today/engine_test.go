package today

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ── fakes ───────────────────────────────────────────────────────────────────

// fakeSource's raw data is a JSON list of strings; each becomes an item keyed
// "<id>:<value>" with one action, "do".
type fakeSource struct {
	id        string
	title     string // the title Build gives the section, if any
	placement Placement
	fetch     func() (json.RawMessage, error)
	act       func(action string, ref ItemRef, raw json.RawMessage) (ActResult, error)
	fetches   atomic.Int32
	acts      atomic.Int32
}

func (f *fakeSource) ID() string    { return f.id }
func (f *fakeSource) Title() string { return strings.ToUpper(f.id) }
func (f *fakeSource) Placement() Placement {
	if f.placement == "" {
		return Column
	}
	return f.placement
}
func (f *fakeSource) Fetch(context.Context, Client) (json.RawMessage, error) {
	f.fetches.Add(1)
	return f.fetch()
}
func (f *fakeSource) Build(raw json.RawMessage, _ time.Time, _ *time.Location) (Built, error) {
	var vals []string
	if err := json.Unmarshal(raw, &vals); err != nil {
		return Built{}, err
	}
	b := Built{Title: f.title, Refs: map[string]ItemRef{}}
	g := Group{Title: f.id}
	for _, v := range vals {
		k := f.id + ":" + v
		g.Items = append(g.Items, Item{Key: k, Title: v, Actions: []Action{{ID: "do", Label: "Do"}}})
		b.Refs[k] = ItemRef{"v": v}
	}
	b.Groups = []Group{g}
	return b, nil
}
func (f *fakeSource) Act(_ context.Context, _ Client, action string, ref ItemRef, raw json.RawMessage) (ActResult, error) {
	f.acts.Add(1)
	if f.act == nil {
		return ActResult{}, ErrNoActions
	}
	return f.act(action, ref, raw)
}

// dayKeyed shows the same titles every day, but its keys carry the date:
// only the keys move.
type dayKeyed struct{ *fakeSource }

func (d dayKeyed) Build(raw json.RawMessage, now time.Time, loc *time.Location) (Built, error) {
	b, err := d.fakeSource.Build(raw, now, loc)
	refs := map[string]ItemRef{}
	for gi := range b.Groups {
		for ii := range b.Groups[gi].Items {
			it := &b.Groups[gi].Items[ii]
			k := it.Key + "@" + now.In(loc).Format(dateLayout)
			refs[k] = b.Refs[it.Key]
			it.Key = k
		}
	}
	b.Refs = refs
	return b, err
}

func answer(raw string) func() (json.RawMessage, error) {
	return func() (json.RawMessage, error) { return json.RawMessage(raw), nil }
}

func fail(err error) func() (json.RawMessage, error) {
	return func() (json.RawMessage, error) { return nil, err }
}

// clock is a now() a test can move; the engine reads it from several goroutines.
type clock struct{ ns atomic.Int64 }

func newClock(t time.Time) *clock { c := &clock{}; c.set(t); return c }
func (c *clock) set(t time.Time)  { c.ns.Store(t.UnixNano()) }
func (c *clock) now() time.Time   { return time.Unix(0, c.ns.Load()).UTC() }

type recorded struct {
	typ  uint32
	body string
}

type recorder struct {
	mu   sync.Mutex
	msgs []recorded
	next int
	wake chan struct{}
}

func newRecorder() *recorder { return &recorder{wake: make(chan struct{}, 4096)} }

func (r *recorder) emit(typ uint32, body string) {
	r.mu.Lock()
	r.msgs = append(r.msgs, recorded{typ, body})
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// waitFor returns the next matching message after the last one it returned.
func (r *recorder) waitFor(t *testing.T, match func(recorded) bool) recorded {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		r.mu.Lock()
		for r.next < len(r.msgs) {
			m := r.msgs[r.next]
			r.next++
			if match(m) {
				r.mu.Unlock()
				return m
			}
		}
		r.mu.Unlock()
		select {
		case <-r.wake:
		case <-deadline:
			t.Fatal("timed out waiting for a message")
			return recorded{}
		}
	}
}

func (r *recorder) all() []recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recorded(nil), r.msgs...)
}

func settled(m recorded) bool {
	return m.typ == MsgToday && strings.Contains(m.body, `"refreshing":false`)
}

func parseSnapshot(t *testing.T, body string) Snapshot {
	t.Helper()
	var s Snapshot
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func (r *recorder) snapshot(t *testing.T) Snapshot {
	t.Helper()
	return parseSnapshot(t, r.waitFor(t, settled).body)
}

func (r *recorder) result(t *testing.T) actResult {
	t.Helper()
	var a actResult
	m := r.waitFor(t, func(m recorded) bool { return m.typ == MsgActResult })
	if err := json.Unmarshal([]byte(m.body), &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func find(s Snapshot, id string) Section {
	for _, x := range s.Sections {
		if x.ID == id {
			return x
		}
	}
	return Section{}
}

func titlesOf(s Section) string {
	var out []string
	for _, g := range s.Groups {
		for _, it := range g.Items {
			out = append(out, it.Title)
		}
	}
	return strings.Join(out, ",")
}

func groupsOf(s Section) string {
	var out []string
	for _, g := range s.Groups {
		out = append(out, g.Title)
	}
	return strings.Join(out, "|")
}

// waitUntil polls cond, for state no message announces yet.
func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for a condition")
		}
		time.Sleep(time.Millisecond)
	}
}

func fixedNow() time.Time { return time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC) }

func newEngine(t *testing.T, srcs ...Source) (*Engine, *recorder, string) {
	t.Helper()
	cache := filepath.Join(t.TempDir(), "today.json")
	rec := newRecorder()
	return New(context.Background(), nil, srcs, time.UTC, cache, rec.emit, fixedNow), rec, cache
}

// cachedAt20 seeds a cache with section id's data, fetched at 20:00 UTC.
func cachedAt20(t *testing.T, path, id, raw string) {
	t.Helper()
	at := time.Date(2026, 10, 6, 20, 0, 0, 0, time.UTC)
	if err := saveCache(path, map[string]cachedSection{id: {Raw: json.RawMessage(raw), FetchedAt: at}}); err != nil {
		t.Fatal(err)
	}
}

// ── behaviour ───────────────────────────────────────────────────────────────

func TestEachSectionSucceedsOrFailsOnItsOwn(t *testing.T) {
	good := &fakeSource{id: "good", fetch: answer(`["a","b"]`)}
	bad := &fakeSource{id: "bad", placement: Banner, fetch: fail(&HTTPError{Status: 503, Message: "personal folder unreadable"})}
	e, rec, cache := newEngine(t, good, bad)
	e.Refresh()
	s := rec.snapshot(t)
	if len(s.Sections) != 2 || s.Sections[0].ID != "good" || s.Sections[1].Placement != Banner {
		t.Fatalf("sections = %+v", s.Sections) // a registered source appears as is
	}
	if g := find(s, "good"); g.Status != "ok" || titlesOf(g) != "a,b" || g.AsOf != "21:00" {
		t.Fatalf("good = %+v", g)
	}
	if b := find(s, "bad"); b.Status != "error" || b.Error != "unavailable: personal folder unreadable" || len(b.Groups) != 0 {
		t.Fatalf("bad = %+v", b)
	}
	c := loadCache(cache)
	if _, ok := c["bad"]; ok || string(c["good"].Raw) != `["a","b"]` {
		t.Fatalf("cache = %v", c)
	}
}

func TestAFailedFetchKeepsTheCachedData(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "today.json")
	cachedAt20(t, cache, "a", `["x"]`)
	a := &fakeSource{id: "a", fetch: fail(&HTTPError{Status: 502})}
	rec := newRecorder()
	e := New(context.Background(), nil, []Source{a}, time.UTC, cache, rec.emit, fixedNow)
	if sec := find(e.Current(), "a"); sec.Status != "ok" || sec.AsOf != "20:00" {
		t.Fatalf("from cache = %+v", sec)
	}
	e.Refresh()
	sec := find(rec.snapshot(t), "a")
	// The view reads "as of 20:00 · offline".
	if sec.Status != "error" || sec.Error != "offline" || titlesOf(sec) != "x" || sec.AsOf != "20:00" {
		t.Fatalf("a = %+v", sec)
	}
	if raw := string(loadCache(cache)["a"].Raw); raw != `["x"]` {
		t.Fatalf("cache = %s", raw)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(cache); err != nil || fi.Mode().Perm() != 0600 {
			t.Fatalf("cache mode = %v, %v", fi.Mode(), err)
		}
	}
}

// Data from an earlier day says which day: after the Mac sleeps overnight,
// "as of 23:50 · offline" would read as tonight. Zones are fixed, so the
// test needs no tzdata; the local day, not UTC's, decides.
func TestAsOfNamesTheDayForOlderData(t *testing.T) {
	brt := time.FixedZone("BRT", -3*3600)
	at := func(day, hour, min int) time.Time { return time.Date(2026, 10, day, hour, min, 0, 0, brt) }
	for _, tc := range []struct {
		name         string
		fetched, now time.Time
		want         string
	}{
		{"two days earlier", at(4, 23, 50), at(6, 21, 0), "Sun 4 Oct 23:50"},
		{"last night, after midnight", at(5, 23, 50), at(6, 0, 10), "Mon 5 Oct 23:50"},
		{"last night, same UTC day", at(6, 23, 50), at(7, 0, 10), "Tue 6 Oct 23:50"},
		{"earlier today", at(6, 8, 15), at(6, 21, 0), "08:15"},
		{"today, past UTC midnight", at(6, 22, 0), at(6, 23, 0), "22:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := filepath.Join(t.TempDir(), "today.json")
			if err := saveCache(cache, map[string]cachedSection{"a": {Raw: json.RawMessage(`["x"]`), FetchedAt: tc.fetched}}); err != nil {
				t.Fatal(err)
			}
			rec := newRecorder()
			a := &fakeSource{id: "a", fetch: fail(&HTTPError{Status: 502})}
			e := New(context.Background(), nil, []Source{a}, brt, cache, rec.emit, func() time.Time { return tc.now })
			if sec := find(e.Current(), "a"); sec.AsOf != tc.want {
				t.Fatalf("from cache: as of %q, want %q", sec.AsOf, tc.want)
			}
			e.Refresh() // the Mac is asleep: the cache stays, with its own time
			if sec := find(rec.snapshot(t), "a"); sec.Error != "offline" || sec.AsOf != tc.want {
				t.Fatalf("after a failed fetch: %+v, want as of %q", sec, tc.want)
			}
		})
	}
}

// The spec's error-handling table, as the engine words it.
func TestFailuresSayWhatTheSpecSays(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		cached bool
		want   string
	}{
		{"Mac asleep, cache present", &HTTPError{Status: 502}, true, "offline"},
		{"Mac asleep, no cache", &HTTPError{Status: 504}, false, "Can't reach the Mac. Today needs the Mac awake."},
		{"request timed out, no cache", context.DeadlineExceeded, false, "Can't reach the Mac. Today needs the Mac awake."},
		{"tailscaled not running, cache present", fmt.Errorf("%w (connection refused)", ErrTailscaleDown), true, "Tailscale isn't running on the tablet"},
		{"tailscaled not running, no cache", fmt.Errorf("%w (connection refused)", ErrTailscaleDown), false, "Tailscale isn't running on the tablet"},
		{"peer not allowed", &HTTPError{Status: 403, Message: "peer not allowed"}, true, "The Mac doesn't recognise this tablet yet. Try again in a minute."},
		{"401", &HTTPError{Status: 401, Message: "bad token"}, false, "Tablet not authorised. Run rm-today-setup."},
		{"another 403", &HTTPError{Status: 403, Message: "route not granted"}, true, "Tablet not authorised. Run rm-today-setup."},
		{"one source fails, cache present", &HTTPError{Status: 503, Message: "personal folder unreadable"}, true, "unavailable: personal folder unreadable"},
		{"one source fails, no cache", errors.New("no mail account could be read (a@example.com)"), false, "unavailable: no mail account could be read (a@example.com)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := filepath.Join(t.TempDir(), "today.json")
			if tc.cached {
				cachedAt20(t, cache, "a", `["x"]`)
			}
			rec := newRecorder()
			e := New(context.Background(), nil, []Source{&fakeSource{id: "a", fetch: fail(tc.err)}}, time.UTC, cache, rec.emit, fixedNow)
			e.Refresh()
			sec := find(rec.snapshot(t), "a")
			if sec.Status != "error" || sec.Error != tc.want {
				t.Fatalf("a = %+v, want error %q", sec, tc.want)
			}
			if tc.cached && (sec.AsOf != "20:00" || titlesOf(sec) != "x") {
				t.Fatalf("cached data lost: %+v", sec)
			}
			if !tc.cached && (sec.AsOf != "" || len(sec.Groups) != 0) {
				t.Fatalf("data from nowhere: %+v", sec)
			}
		})
	}
}

func TestACorruptCacheIsAnEmptyCache(t *testing.T) {
	for name, body := range map[string]string{
		"future version": `{"version":9,"sections":{"a":{"raw":["x"]}}}`,
		"not JSON":       `{`,
	} {
		t.Run(name, func(t *testing.T) {
			cache := filepath.Join(t.TempDir(), "today.json")
			if err := os.WriteFile(cache, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			a := &fakeSource{id: "a", fetch: answer(`["y"]`)}
			e := New(context.Background(), nil, []Source{a}, time.UTC, cache, newRecorder().emit, fixedNow)
			if sec := find(e.Current(), "a"); sec.Status != "none" || len(sec.Groups) != 0 {
				t.Fatalf("a = %+v", sec)
			}
		})
	}
}

func TestARefreshWhileOneRunsIsIgnored(t *testing.T) {
	release := make(chan struct{})
	a := &fakeSource{id: "a", fetch: func() (json.RawMessage, error) { <-release; return json.RawMessage(`["x"]`), nil }}
	e, rec, _ := newEngine(t, a)
	e.Refresh()
	e.Refresh()
	close(release)
	rec.snapshot(t)
	time.Sleep(50 * time.Millisecond)
	if n := a.fetches.Load(); n != 1 {
		t.Fatalf("fetches = %d, want 1", n)
	}
}

func TestActionsNameTheRevTheyWereDrawnFrom(t *testing.T) {
	var acted atomic.Int32
	a := &fakeSource{id: "a", fetch: answer(`["x"]`), act: func(string, ItemRef, json.RawMessage) (ActResult, error) {
		acted.Add(1)
		return ActResult{}, nil
	}}
	e, rec, _ := newEngine(t, a)
	e.Refresh()
	rev := find(rec.snapshot(t), "a").Rev
	e.Act("a", rev-1, "do", "a:x")
	if r := rec.result(t); r.OK || r.Message != "this list changed; check it again" {
		t.Fatalf("stale rev = %+v", r)
	}
	e.Act("a", rev, "do", "a:gone")
	if r := rec.result(t); r.OK || r.Message != "this item is no longer there" {
		t.Fatalf("unknown key = %+v", r)
	}
	e.Act("zzz", rev, "do", "a:x")
	if r := rec.result(t); r.OK || r.Message != "unknown section" {
		t.Fatalf("unknown section = %+v", r)
	}
	if n := acted.Load(); n != 0 {
		t.Fatalf("the source acted %d times", n)
	}
}

func TestASecondActionWhileOneRunsIsRefused(t *testing.T) {
	release := make(chan struct{})
	a := &fakeSource{id: "a", fetch: answer(`["x","y"]`), act: func(string, ItemRef, json.RawMessage) (ActResult, error) {
		<-release
		return ActResult{Message: "Done"}, nil
	}}
	e, rec, _ := newEngine(t, a)
	e.Refresh()
	rev := find(rec.snapshot(t), "a").Rev
	e.Act("a", rev, "do", "a:x")
	e.Act("a", rev, "do", "a:y")
	if r := rec.result(t); r.OK || r.Key != "a:y" || r.Message != "another action is in progress" {
		t.Fatalf("second = %+v", r)
	}
	close(release)
	if r := rec.result(t); !r.OK || r.Key != "a:x" || r.Message != "Done" {
		t.Fatalf("first = %+v", r)
	}
}

func TestAFetchThatStartedBeforeAnActionCannotUndoIt(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	a := &fakeSource{id: "a", fetch: func() (json.RawMessage, error) {
		if calls.Add(1) > 1 {
			<-release // the stale fetch: x was ticked while it ran
		}
		return json.RawMessage(`["x","y"]`), nil
	}, act: func(string, ItemRef, json.RawMessage) (ActResult, error) {
		return ActResult{Raw: json.RawMessage(`["y"]`)}, nil
	}}
	e, rec, cache := newEngine(t, a)
	e.Refresh()
	rev := find(rec.snapshot(t), "a").Rev
	e.Refresh()
	e.Act("a", rev, "do", "a:x")
	if r := rec.result(t); !r.OK || r.Message != "Done" {
		t.Fatalf("result = %+v", r)
	}
	close(release)
	if got := titlesOf(find(rec.snapshot(t), "a")); got != "y" {
		t.Fatalf("a = %s, want y", got)
	}
	if raw := string(loadCache(cache)["a"].Raw); raw != `["y"]` {
		t.Fatalf("cache = %s", raw)
	}
}

func TestRefetchRequestsAreASetRunOnceAfterTheRunningFetch(t *testing.T) {
	release := make(chan struct{})
	mk := func(id string) *fakeSource {
		var calls atomic.Int32
		return &fakeSource{id: id, fetch: func() (json.RawMessage, error) {
			if calls.Add(1) == 2 {
				<-release
			}
			return json.RawMessage(`["x"]`), nil
		}, act: func(string, ItemRef, json.RawMessage) (ActResult, error) {
			return ActResult{Refetch: true}, &HTTPError{Status: 409, Message: "file changed, retry"}
		}}
	}
	a, b := mk("a"), mk("b")
	e, rec, _ := newEngine(t, a, b)
	e.Refresh()
	s := rec.snapshot(t)
	e.Refresh() // the second fetch of each blocks
	for _, id := range []string{"a", "b", "a"} {
		e.Act(id, find(s, id).Rev, "do", id+":x")
		if r := rec.result(t); r.OK || r.Message != "file changed, retry" {
			t.Fatalf("result = %+v", r)
		}
	}
	close(release)
	rec.snapshot(t) // the stale second fetch settles, its results dropped
	rec.snapshot(t) // the one refetch of {a, b} settles
	time.Sleep(50 * time.Millisecond)
	if na, nb := a.fetches.Load(), b.fetches.Load(); na != 3 || nb != 3 {
		t.Fatalf("fetches a=%d b=%d, want exactly 3 each", na, nb)
	}
}

// A tick 409 fails the action and still asks for one refetch.
func TestARefusedActionThatAsksForARefetchGetsOne(t *testing.T) {
	a := &fakeSource{id: "a", fetch: answer(`["x"]`), act: func(string, ItemRef, json.RawMessage) (ActResult, error) {
		return ActResult{Refetch: true}, &HTTPError{Status: 409, Message: "no open item matches"}
	}}
	e, rec, _ := newEngine(t, a)
	e.Refresh()
	rev := find(rec.snapshot(t), "a").Rev
	e.Act("a", rev, "do", "a:x")
	if r := rec.result(t); r.OK || r.Message != "no open item matches" {
		t.Fatalf("result = %+v", r)
	}
	rec.snapshot(t) // the state after the action
	rec.snapshot(t) // the refetch settles
	time.Sleep(50 * time.Millisecond)
	if n := a.fetches.Load(); n != 2 {
		t.Fatalf("fetches = %d, want 2", n)
	}
}

// A fetch whose data Build rejects counts as failed: the last good data
// stays, in the view and in the cache.
func TestAFetchWhoseDataCannotBeBuiltKeepsTheLastGoodData(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "today.json")
	cachedAt20(t, cache, "a", `["x","y"]`)
	a := &fakeSource{id: "a", fetch: answer(`{"not":"a list"}`)}
	rec := newRecorder()
	e := New(context.Background(), nil, []Source{a}, time.UTC, cache, rec.emit, fixedNow)
	before := find(e.Current(), "a")
	e.Refresh()
	sec := find(rec.snapshot(t), "a")
	if sec.Status != "error" || !strings.HasPrefix(sec.Error, "unavailable: ") {
		t.Fatalf("a = %+v", sec)
	}
	if titlesOf(sec) != "x,y" || sec.AsOf != "20:00" || sec.Rev != before.Rev {
		t.Fatalf("the last good data was replaced: %+v", sec)
	}
	if raw := string(loadCache(cache)["a"].Raw); raw != `["x","y"]` {
		t.Fatalf("cache = %s", raw)
	}
}

// A rebuild that only moves keys is a change: the old sheet is refused.
func TestARebuildThatOnlyMovesKeysChangesTheRev(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "today.json")
	cachedAt20(t, cache, "a", `["x"]`)
	src := dayKeyed{&fakeSource{id: "a", fetch: fail(&HTTPError{Status: 502}), act: func(string, ItemRef, json.RawMessage) (ActResult, error) {
		return ActResult{}, nil
	}}}
	clk := newClock(fixedNow())
	rec := newRecorder()
	e := New(context.Background(), nil, []Source{src}, time.UTC, cache, rec.emit, clk.now)
	before := find(e.Current(), "a")
	clk.set(fixedNow().Add(4 * time.Hour)) // past midnight UTC
	e.Refresh()
	after := find(rec.snapshot(t), "a")
	if titlesOf(after) != titlesOf(before) || after.Groups[0].Items[0].Key == before.Groups[0].Items[0].Key {
		t.Fatalf("the test needs equal titles and moved keys: %+v → %+v", before.Groups, after.Groups)
	}
	if after.Rev == before.Rev {
		t.Fatal("keys moved without a new rev")
	}
	e.Act("a", before.Rev, "do", before.Groups[0].Items[0].Key)
	if r := rec.result(t); r.OK || r.Message != "this list changed; check it again" {
		t.Fatalf("old sheet = %+v", r)
	}
	if n := src.acts.Load(); n != 0 {
		t.Fatalf("the source acted %d times", n)
	}
}

func TestABuiltTitleNamesTheSection(t *testing.T) {
	titled := &fakeSource{id: "a", title: "Bom dia, segunda", fetch: answer(`["x"]`)}
	untitled := &fakeSource{id: "b", fetch: answer(`["y"]`)}
	failed := &fakeSource{id: "c", title: "never built", fetch: fail(errors.New("boom"))}
	e, rec, _ := newEngine(t, titled, untitled, failed)
	e.Refresh()
	s := rec.snapshot(t)
	if a, b, c := find(s, "a").Title, find(s, "b").Title, find(s, "c").Title; a != "Bom dia, segunda" || b != "B" || c != "C" {
		t.Fatalf("titles = %q, %q, %q", a, b, c)
	}
}

func TestNoSectionSendsNullGroups(t *testing.T) {
	ok := &fakeSource{id: "ok", fetch: answer(`["x"]`)}
	bad := &fakeSource{id: "bad", fetch: fail(errors.New("boom"))}
	e, rec, _ := newEngine(t, ok, bad)
	none := mustJSON(e.Current()) // nothing fetched yet: every section is "none"
	e.Refresh()
	if b := find(rec.snapshot(t), "bad"); b.Status != "error" {
		t.Fatalf("bad = %+v", b)
	}
	e.ClearCache()
	rec.snapshot(t)
	bodies := []string{none}
	for _, m := range rec.all() {
		bodies = append(bodies, m.body)
	}
	for _, body := range bodies {
		if strings.Contains(body, `"groups":null`) {
			t.Fatalf("null groups in %s", body)
		}
	}
	if !strings.Contains(none, `"groups":[]`) {
		t.Fatalf("none = %s", none)
	}
}

func TestAnActionOnCachedDataResolvesAfterARestart(t *testing.T) {
	first := &fakeSource{id: "a", fetch: answer(`["x"]`)}
	e1, rec, cache := newEngine(t, first)
	e1.Refresh()
	rec.snapshot(t)

	var got ItemRef
	second := &fakeSource{id: "a", fetch: fail(errors.New("not called")), act: func(_ string, ref ItemRef, _ json.RawMessage) (ActResult, error) {
		got = ref
		return ActResult{}, nil
	}}
	rec2 := newRecorder()
	e2 := New(context.Background(), nil, []Source{second}, time.UTC, cache, rec2.emit, fixedNow)
	e2.Act("a", find(e2.Current(), "a").Rev, "do", "a:x")
	if r := rec2.result(t); !r.OK {
		t.Fatalf("result = %+v", r)
	}
	if got["v"] != "x" {
		t.Fatalf("ref = %v", got)
	}
}

func TestClearCacheForgetsEverything(t *testing.T) {
	a := &fakeSource{id: "a", fetch: answer(`["x"]`)}
	e, rec, cache := newEngine(t, a)
	e.Refresh()
	before := find(rec.snapshot(t), "a")
	e.ClearCache()
	after := find(rec.snapshot(t), "a")
	if after.Status != "none" || len(after.Groups) != 0 || after.Rev == before.Rev {
		t.Fatalf("after = %+v", after)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatalf("cache still there: %v", err)
	}
}

// Opening Today refreshes it. A refresh that changes nothing keeps the rev,
// so the sheet on screen still acts; one that changes the list refuses it.
// A sibling is held mid-fetch so the action runs before the batch's 109.
func TestARefreshThatChangesNothingKeepsTheRev(t *testing.T) {
	for _, tc := range []struct {
		name    string
		second  string
		acts    int32
		message string
	}{
		{"identical data", `["x"]`, 1, "Done"},
		{"changed data", `["x","y"]`, 0, "this list changed; check it again"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var data atomic.Value
			data.Store(`["x"]`)
			a := &fakeSource{id: "a", fetch: func() (json.RawMessage, error) {
				return json.RawMessage(data.Load().(string)), nil
			}, act: func(string, ItemRef, json.RawMessage) (ActResult, error) {
				return ActResult{Message: "Done"}, nil
			}}
			release := make(chan struct{})
			var calls atomic.Int32
			sibling := &fakeSource{id: "b", fetch: func() (json.RawMessage, error) {
				if calls.Add(1) > 1 {
					<-release
				}
				return json.RawMessage(`["x"]`), nil
			}}
			clk := newClock(fixedNow())
			rec := newRecorder()
			e := New(context.Background(), nil, []Source{a, sibling}, time.UTC,
				filepath.Join(t.TempDir(), "today.json"), rec.emit, clk.now)
			e.Refresh()
			rev := find(rec.snapshot(t), "a").Rev

			data.Store(tc.second)
			clk.set(fixedNow().Add(5 * time.Minute))
			e.Refresh()
			// a's fetch has landed (its time moved); the sibling still blocks.
			waitUntil(t, func() bool { return find(e.Current(), "a").AsOf == "21:05" })
			e.Act("a", rev, "do", "a:x")
			if r := rec.result(t); r.OK != (tc.acts == 1) || r.Message != tc.message {
				t.Fatalf("act = %+v", r)
			}
			close(release)
			rec.snapshot(t)
			if n := a.acts.Load(); n != tc.acts {
				t.Fatalf("the source acted %d times, want %d", n, tc.acts)
			}
		})
	}
}

// Revs start from the clock: a sheet drawn before a restart never matches
// one drawn after it, even from the same cache.
func TestARevFromBeforeARestartIsRefused(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "today.json")
	cachedAt20(t, cache, "a", `["x"]`)
	first := New(context.Background(), nil, []Source{&fakeSource{id: "a", fetch: answer(`["x"]`)}},
		time.UTC, cache, newRecorder().emit, fixedNow)
	rev := find(first.Current(), "a").Rev

	second := &fakeSource{id: "a", fetch: answer(`["x"]`), act: func(string, ItemRef, json.RawMessage) (ActResult, error) {
		return ActResult{}, nil
	}}
	rec := newRecorder()
	later := func() time.Time { return fixedNow().Add(time.Minute) }
	e := New(context.Background(), nil, []Source{second}, time.UTC, cache, rec.emit, later)
	e.Act("a", rev, "do", "a:x")
	if r := rec.result(t); r.OK || r.Message != "this list changed; check it again" {
		t.Fatalf("old rev = %+v", r)
	}
	if n := second.acts.Load(); n != 0 {
		t.Fatalf("the source acted %d times", n)
	}
}

// Clear cache is a privacy control: nothing begun before it brings the data
// back, in the view or on disk.
func TestClearCacheDropsWorkAlreadyRunning(t *testing.T) {
	gone := func(t *testing.T, sec Section, cache string) {
		t.Helper()
		if sec.Status != "none" || len(sec.Groups) != 0 || sec.AsOf != "" {
			t.Fatalf("after the clear = %+v", sec)
		}
		if _, err := os.Stat(cache); !os.IsNotExist(err) {
			t.Fatalf("the cache came back: %v", err)
		}
	}
	blockedAfterFirst := func(release chan struct{}) func() (json.RawMessage, error) {
		var calls atomic.Int32
		return func() (json.RawMessage, error) {
			if calls.Add(1) > 1 {
				<-release
			}
			return json.RawMessage(`["x"]`), nil
		}
	}

	t.Run("an action", func(t *testing.T) {
		release := make(chan struct{})
		a := &fakeSource{id: "a", fetch: answer(`["x"]`), act: func(string, ItemRef, json.RawMessage) (ActResult, error) {
			<-release
			return ActResult{Raw: json.RawMessage(`["y"]`), Message: "Done"}, nil
		}}
		e, rec, cache := newEngine(t, a)
		e.Refresh()
		rev := find(rec.snapshot(t), "a").Rev
		e.Act("a", rev, "do", "a:x")
		e.ClearCache()
		rec.snapshot(t)
		close(release)
		if r := rec.result(t); !r.OK {
			t.Fatalf("action = %+v", r) // it did happen at the gateway
		}
		gone(t, find(rec.snapshot(t), "a"), cache)
	})

	t.Run("a fetch", func(t *testing.T) {
		release := make(chan struct{})
		a := &fakeSource{id: "a", fetch: blockedAfterFirst(release)}
		e, rec, cache := newEngine(t, a)
		e.Refresh()
		rec.snapshot(t)
		e.Refresh() // blocks
		e.ClearCache()
		close(release)
		gone(t, find(rec.snapshot(t), "a"), cache)
	})

	t.Run("a refetch queued before it", func(t *testing.T) {
		release := make(chan struct{})
		a := &fakeSource{id: "a", fetch: blockedAfterFirst(release), act: func(string, ItemRef, json.RawMessage) (ActResult, error) {
			return ActResult{Refetch: true}, &HTTPError{Status: 409, Message: "file changed, retry"}
		}}
		e, rec, cache := newEngine(t, a)
		e.Refresh()
		rev := find(rec.snapshot(t), "a").Rev
		e.Refresh() // blocks
		e.Act("a", rev, "do", "a:x")
		if r := rec.result(t); r.OK {
			t.Fatalf("result = %+v", r)
		}
		e.ClearCache()
		close(release)
		sec := find(rec.snapshot(t), "a")
		time.Sleep(50 * time.Millisecond)
		if n := a.fetches.Load(); n != 2 {
			t.Fatalf("fetches = %d, want 2: the queued refetch ran after the clear", n)
		}
		gone(t, sec, cache)
	})
}

func TestNotConfiguredSaysWhy(t *testing.T) {
	var s Snapshot
	if err := json.Unmarshal([]byte(NotConfigured("today.json: timezone is required")), &s); err != nil {
		t.Fatal(err)
	}
	if s.Configured || s.Problem != "today.json: timezone is required" || s.Sections == nil {
		t.Fatalf("snapshot = %+v", s)
	}
}

// screened is a fakeSource whose model also carries a Screen.
type screened struct{ *fakeSource }

func (s screened) Build(raw json.RawMessage, now time.Time, loc *time.Location) (Built, error) {
	b, err := s.fakeSource.Build(raw, now, loc)
	b.Screen = &Screen{Cols: 2, Rows: 1, Banners: []Cell{},
		Cells: []Cell{{View: "stat", W: 1, H: 1, Title: "x", Tone: "neutral"}}}
	return b, err
}

func TestASectionCarriesItsScreen(t *testing.T) {
	e, rec, _ := newEngine(t,
		screened{&fakeSource{id: "a", fetch: answer(`["x"]`)}},
		&fakeSource{id: "b", fetch: answer(`["y"]`)})
	e.Refresh()
	snap := rec.snapshot(t)
	sec := find(snap, "a")
	if sec.Screen == nil || len(sec.Screen.Cells) != 1 || sec.Screen.Cells[0].Title != "x" {
		t.Fatalf("screen = %+v", sec.Screen)
	}
	if !strings.Contains(mustJSON(sec), `"screen":{"cols":2,"rows":1`) {
		t.Fatalf("109 section = %s", mustJSON(sec))
	}
	// A section without a screen sends no "screen" key at all, not null.
	if b := mustJSON(find(snap, "b")); strings.Contains(b, `"screen"`) {
		t.Fatalf("a section without a screen sends one: %s", b)
	}
}
