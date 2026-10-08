package today

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 09:30 on Wednesday 7 October in São Paulo.
var widgetsNow = time.Date(2026, 10, 7, 12, 30, 0, 0, time.UTC)

func spLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// waterItem is the first task, kept separate so a test can remove it.
const waterItem = `{"key":"t:41","title":"Water the plants","subtitle":"due today","tone":"warn","actions":["tick","purge","snooze"]},`

// screenJSON is a GET /screens/tablet answer with every view, from an
// invented app "demo". The layout lists its placements out of reading order.
const screenJSON = `{"screen":"tablet",
 "layout":{"screen":"tablet","rev":3,"grid":{"cols":2},"items":[
  {"widget":"demo.money","view":"stat","x":1,"y":1,"w":1,"h":1},
  {"widget":"demo.notes","view":"alert","x":0,"y":0,"w":2,"h":1},
  {"widget":"demo.tasks","view":"list","x":0,"y":1,"w":1,"h":2},
  {"widget":"demo.bots","view":"spark","x":1,"y":2,"w":1,"h":1}]},
 "widgets":{
  "demo.notes":{"widget":"demo.notes","app":"demo","title":"Notes","views":["alert"],"state":"ok",
   "data":{"id":"notes","title":"Notes","as_of":"2026-10-07T09:00:00-03:00",
    "alert":{"text":"Invoice closes in 2 days","tone":"warn"}},"actions":[]},
  "demo.tasks":{"widget":"demo.tasks","app":"demo","title":"Tasks","views":["list","stat"],"state":"ok",
   "data":{"id":"tasks","title":"Tasks","as_of":"2026-10-07T09:00:00-03:00",
    "list":{"groups":[{"title":"Today","items":[` + waterItem + `
     {"key":"t:42","title":"Call the bank","actions":["archive","skip"]}]}]}},
   "actions":[
    {"id":"tick","label":"Done","scope":"item","risk":"low"},
    {"id":"archive","label":"Archive","scope":"item","risk":"medium","confirm":"Archive {title}?"},
    {"id":"skip","label":"Skip this month","scope":"item","risk":"medium"},
    {"id":"purge","label":"Purge","scope":"item","risk":"high"}]},
  "demo.money":{"widget":"demo.money","app":"demo","title":"Money","views":["stat"],"state":"stale",
   "data":{"id":"money","title":"Money","as_of":"2026-10-07T08:30:00-03:00","tone":"warn",
    "stat":{"value":"1.234","label":"Balance","delta":"−56 this week","tone":"bad"}},"actions":[]},
  "demo.bots":{"widget":"demo.bots","app":"demo","title":"Bots","views":["spark"],"state":"ok",
   "data":{"id":"bots","title":"Bots","as_of":"2026-10-07T09:00:00-03:00",
    "spark":{"label":"Equity","points":[10,20,15],"unit":"USD"}},"actions":[]}}}`

func buildScreen(t *testing.T, body string) Built {
	t.Helper()
	c := &fakeClient{get: map[string]string{"/screens/tablet": body}}
	raw, err := WidgetsSource{}.Fetch(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	b, err := WidgetsSource{}.Build(raw, widgetsNow, spLoc(t))
	if err != nil {
		t.Fatal(err)
	}
	if b.Screen == nil {
		t.Fatal("no screen")
	}
	return b
}

func screenOf(t *testing.T, body string) *Screen { t.Helper(); return buildScreen(t, body).Screen }

// oneEach lays out one small stat cell per entry, two to a row, in the
// order given; entry i is widget demo.w<i>.
func oneEach(entries ...string) string {
	var items, widgets []string
	for i, e := range entries {
		id := fmt.Sprintf("demo.w%d", i)
		items = append(items, fmt.Sprintf(`{"widget":%q,"view":"stat","x":%d,"y":%d,"w":1,"h":1}`, id, i%2, i/2))
		widgets = append(widgets, fmt.Sprintf("%q:%s", id, e))
	}
	return fmt.Sprintf(`{"layout":{"grid":{"cols":2},"items":[%s]},"widgets":{%s}}`,
		strings.Join(items, ","), strings.Join(widgets, ","))
}

const okStat = `"stat":{"value":"7","label":"Open"}`

func viewsSet(c Cell) int {
	n := 0
	for _, set := range []bool{c.Stat != nil, c.Groups != nil, c.Spark != nil, c.Alert != nil, c.Metrics != nil} {
		if set {
			n++
		}
	}
	return n
}

func TestEveryViewIsMapped(t *testing.T) {
	s := screenOf(t, screenJSON)
	if s.Cols != 2 || s.Rows != 2 || len(s.Banners) != 1 || len(s.Cells) != 3 {
		t.Fatalf("screen = %+v", s)
	}
	if b := s.Banners[0]; b.Title != "Notes" || b.Alert == nil ||
		*b.Alert != (Alert{Text: "Invoice closes in 2 days", Tone: "warn"}) {
		t.Fatalf("banner = %+v", b)
	}
	// Reading order, and row 0 (only the alert used it) closed.
	var got []string
	for _, c := range s.Cells {
		got = append(got, fmt.Sprintf("%s %s %d,%d %dx%d", c.Title, c.View, c.X, c.Y, c.W, c.H))
	}
	want := []string{"Tasks list 0,0 1x2", "Money stat 1,0 1x1", "Bots spark 1,1 1x1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cells\n got %q\nwant %q", got, want)
	}
	for _, c := range append(append([]Cell{}, s.Banners...), s.Cells...) {
		if viewsSet(c) != 1 {
			t.Fatalf("%s %s carries %d views", c.Title, c.View, viewsSet(c))
		}
	}
	money, spark := s.Cells[1], s.Cells[2]
	if *money.Stat != (Stat{Value: "1.234", Label: "Balance", Delta: "−56 this week", Trend: "down", Tone: "bad"}) || money.Tone != "warn" {
		t.Fatalf("stat = %+v, tone %s", money.Stat, money.Tone)
	}
	if !reflect.DeepEqual(*spark.Spark, Spark{Label: "Equity", Points: []float64{0, 1, 0.5}, Min: "10", Max: "20", Last: "15", Unit: "USD"}) {
		t.Fatalf("spark = %+v", spark.Spark)
	}
}

func TestListItemsKeepStableKeysAndOfferOnlyLowAndMediumActions(t *testing.T) {
	b := buildScreen(t, screenJSON)
	tasks := b.Screen.Cells[0]
	if len(tasks.Groups) != 1 || tasks.Groups[0].Title != "Today" || len(tasks.Groups[0].Items) != 2 {
		t.Fatalf("groups = %+v", tasks.Groups)
	}
	water, bank := tasks.Groups[0].Items[0], tasks.Groups[0].Items[1]
	if water.Key != "demo.tasks t:41" || water.Title != "Water the plants" || water.Subtitle != "due today" || water.Tone != "warn" {
		t.Fatalf("water = %+v", water)
	}
	// purge is high and snooze is not in the catalog: neither is offered.
	if !reflect.DeepEqual(water.Actions, []Action{{ID: "tick", Label: "Done", Risk: "low"}}) {
		t.Fatalf("water actions = %+v", water.Actions)
	}
	want := []Action{
		{ID: "archive", Label: "Archive", Risk: "medium", Confirm: "Archive Call the bank?"},
		{ID: "skip", Label: "Skip this month", Risk: "medium", Confirm: "Skip this month: Call the bank?"},
	}
	if !reflect.DeepEqual(bank.Actions, want) || bank.Tone != "neutral" {
		t.Fatalf("bank = %+v", bank)
	}
	ref := b.Refs[water.Key]
	if ref["widget"] != "demo.tasks" || ref["key"] != "t:41" || ref["title"] != "Tasks" || ref["actions"] != "tick" {
		t.Fatalf("water ref = %v", ref)
	}
	if b.Refs[bank.Key]["actions"] != "archive skip" {
		t.Fatalf("bank ref = %v", b.Refs[bank.Key])
	}
}

func TestWidgetStatesShowInTheFrame(t *testing.T) {
	s := screenOf(t, oneEach(
		`{"title":"Fresh","state":"ok","data":{"id":"w","title":"Fresh","as_of":"2026-10-07T09:00:00-03:00",`+okStat+`},"actions":[]}`,
		`{"title":"Cached","state":"stale","data":{"id":"w","title":"Cached","as_of":"2026-10-07T08:30:00-03:00",`+okStat+`},"actions":[]}`,
		`{"title":"Last night","state":"stale","data":{"id":"w","title":"Last night","as_of":"2026-10-06T23:50:00-03:00",`+okStat+`},"actions":[]}`,
		`{"title":"Down","state":"error","error":"unavailable: Demo is down","actions":[]}`,
		`{"title":"Bare","state":"error","error":"bad data from Demo","actions":[]}`,
		`{"title":"Silent","state":"error","actions":[]}`,
		`{"title":"Odd","state":"sideways","actions":[]}`,
	))
	for i, w := range []struct {
		note          string
		problem, stat bool
	}{
		{"", false, true},
		{"as of 08:30 · offline", false, true},
		{"as of Tue 6 Oct 23:50 · offline", false, true},
		{"unavailable: Demo is down", true, false},
		{"unavailable: bad data from Demo", true, false},
		{"unavailable", true, false},
		{"unavailable", true, false},
	} {
		c := s.Cells[i]
		if c.Note != w.note || c.Problem != w.problem || (c.Stat != nil) != w.stat {
			t.Errorf("cell %d (%s) = note %q problem %v stat %v; want %+v", i, c.Title, c.Note, c.Problem, c.Stat != nil, w)
		}
	}
}

func TestStaleWordingFollowsTheClockAcrossMidnight(t *testing.T) {
	c := &fakeClient{get: map[string]string{"/screens/tablet": oneEach(
		`{"title":"Cached","state":"stale","data":{"id":"w","title":"Cached","as_of":"2026-10-06T23:50:00-03:00",` + okStat + `},"actions":[]}`)}}
	raw, err := WidgetsSource{}.Fetch(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	loc := spLoc(t)
	for _, tc := range []struct {
		now  time.Time
		want string
	}{
		{time.Date(2026, 10, 6, 23, 55, 0, 0, loc), "as of 23:50 · offline"},
		{time.Date(2026, 10, 7, 0, 10, 0, 0, loc), "as of Tue 6 Oct 23:50 · offline"},
	} {
		b, err := WidgetsSource{}.Build(raw, tc.now, loc)
		if err != nil || b.Screen.Cells[0].Note != tc.want {
			t.Fatalf("at %v: note %q, %v; want %q", tc.now, b.Screen.Cells[0].Note, err, tc.want)
		}
	}
}

func TestARemovedWidgetShowsUnavailable(t *testing.T) {
	s := screenOf(t, `{"layout":{"grid":{"cols":2},"items":[
	 {"widget":"demo.gone","view":"list","x":0,"y":0,"w":1,"h":1},
	 {"widget":"demo.here","view":"stat","x":1,"y":0,"w":1,"h":1}]},
	 "widgets":{"demo.here":{"title":"Here","state":"ok","data":{"id":"here","title":"Here","as_of":"2026-10-07T09:00:00-03:00",`+okStat+`},"actions":[]}}}`)
	gone, here := s.Cells[0], s.Cells[1]
	if gone.Title != "demo.gone" || !gone.Problem || gone.Note != "unavailable: no data for this widget" || gone.Groups != nil {
		t.Fatalf("gone = %+v", gone)
	}
	if here.Problem || here.Stat == nil {
		t.Fatalf("here = %+v", here)
	}
}

// "table" was a view once and was dropped: a gateway that still sends it, or
// any view this tablet doesn't know, gets "unavailable" and never a crash.
func TestAnUnknownViewShowsUnavailable(t *testing.T) {
	s := screenOf(t, `{"layout":{"grid":{"cols":2},"items":[{"widget":"demo.w","view":"table","x":0,"y":0,"w":2,"h":2}]},
	 "widgets":{"demo.w":{"title":"Dial","state":"ok","data":{"id":"w","title":"Dial","as_of":"2026-10-07T09:00:00-03:00",`+okStat+`,
	  "table":{"columns":["Bot","P&L"],"rows":[["alpha","+1.2"]]}},"actions":[]}}}`)
	c := s.Cells[0]
	if c.Title != "Dial" || !c.Problem || c.Note != `unavailable: this tablet can't show the "table" view` || viewsSet(c) != 0 {
		t.Fatalf("cell = %+v", c)
	}
	if !strings.Contains(mustJSON(s), `"view":"table"`) {
		t.Fatal("the cell did not reach the view")
	}
}

func TestAnUnreadableWidgetEntryFailsOnlyItsCell(t *testing.T) {
	s := screenOf(t, oneEach(
		`{"title":"Broken","state":"ok","data":{"id":"b","title":"Broken","as_of":"2026-10-07T09:00:00-03:00","stat":{"value":5,"label":"n"}},"actions":[]}`,
		`{"title":"Fine","state":"ok","data":{"id":"f","title":"Fine","as_of":"2026-10-07T09:00:00-03:00",`+okStat+`},"actions":[]}`,
	))
	broken, fine := s.Cells[0], s.Cells[1]
	if broken.Title != "demo.w0" || !broken.Problem || broken.Note != "unavailable: this tablet can't read its data" {
		t.Fatalf("broken = %+v", broken)
	}
	if fine.Problem || fine.Stat == nil || fine.Stat.Value != "7" {
		t.Fatalf("fine = %+v", fine)
	}
}

func TestRowsNothingUsesAreClosed(t *testing.T) {
	s := screenOf(t, `{"layout":{"grid":{"cols":2},"items":[
	 {"widget":"demo.a","view":"stat","x":0,"y":5,"w":2,"h":1},
	 {"widget":"demo.a","view":"stat","x":1,"y":1,"w":1,"h":2}]},
	 "widgets":{"demo.a":{"title":"A","state":"ok","data":{"id":"a","title":"A","as_of":"2026-10-07T09:00:00-03:00",`+okStat+`},"actions":[]}}}`)
	if s.Rows != 3 || s.Cells[0].Y != 0 || s.Cells[0].H != 2 || s.Cells[1].Y != 2 {
		t.Fatalf("screen = %+v", s)
	}
}

func TestPlacementsOutsideTheGridAreLeftOutWithAWarning(t *testing.T) {
	b := buildScreen(t, `{"layout_warning":"the stored layout was unreadable; showing the last good one",
	 "layout":{"grid":{"cols":2},"items":[
	  {"widget":"demo.a","view":"stat","x":1,"y":0,"w":2,"h":1},
	  {"widget":"demo.a","view":"stat","x":0,"y":0,"w":0,"h":1},
	  {"widget":"demo.a","view":"stat","x":0,"y":23,"w":1,"h":2},
	  {"widget":"demo.a","view":"stat","x":0,"y":0,"w":1,"h":1}]},
	 "widgets":{"demo.a":{"title":"A","state":"ok","data":{"id":"a","title":"A","as_of":"2026-10-07T09:00:00-03:00",`+okStat+`},"actions":[]}}}`)
	if len(b.Screen.Cells) != 1 || b.Warning != "the stored layout was unreadable; showing the last good one · 3 widgets outside the grid were left out" {
		t.Fatalf("cells %d, warning %q", len(b.Screen.Cells), b.Warning)
	}
}

func TestAnAlertWithNothingToSayIsNoBanner(t *testing.T) {
	s := screenOf(t, `{"layout":{"grid":{"cols":2},"items":[
	 {"widget":"demo.quiet","view":"alert","x":0,"y":0,"w":2,"h":1},
	 {"widget":"demo.down","view":"alert","x":0,"y":1,"w":2,"h":1}]},
	 "widgets":{
	  "demo.quiet":{"title":"Quiet","state":"ok","data":{"id":"q","title":"Quiet","as_of":"2026-10-07T09:00:00-03:00"},"actions":[]},
	  "demo.down":{"title":"Down","state":"error","error":"unavailable: Demo is down","actions":[]}}}`)
	if len(s.Banners) != 1 || s.Banners[0].Title != "Down" || !s.Banners[0].Problem || len(s.Cells) != 0 || s.Rows != 0 {
		t.Fatalf("screen = %+v", s)
	}
}

func TestToneAndTrend(t *testing.T) {
	for in, want := range map[string]string{"good": "good", "warn": "warn", "bad": "bad", "neutral": "neutral", "": "neutral", "loud": "neutral"} {
		if got := tone(in); got != want {
			t.Errorf("tone(%q) = %q, want %q", in, got, want)
		}
	}
	for delta, want := range map[string]string{"+3 today": "up", " -3": "down", "−R$ 1.900": "down", "3": "", "": ""} {
		if got := buildStat(&statView{Value: "1", Label: "x", Delta: delta}).Trend; got != want {
			t.Errorf("trend(%q) = %q, want %q", delta, got, want)
		}
	}
}

func TestSparkScaling(t *testing.T) {
	flat := buildSpark(&sparkView{Label: "x", Points: []float64{5, 5}})
	if !reflect.DeepEqual(flat.Points, []float64{0.5, 0.5}) || flat.Min != "5" || flat.Max != "5" {
		t.Fatalf("flat = %+v", flat)
	}
	if buildSpark(&sparkView{Label: "x", Points: []float64{5}}) != nil {
		t.Fatal("one point drew a line")
	}
	// A range the arithmetic cannot normalise directly still draws, and still
	// marshals: one NaN or Inf point fails json.Marshal and blanks the whole
	// screen. Too wide: hi-lo overflows. Too narrow: the span is subnormal.
	for _, c := range []struct {
		name   string
		points []float64
		want   []float64
	}{
		{"too wide", []float64{-1.7e308, 0, 1.7e308}, []float64{0, 0.5, 1}},
		{"too narrow", []float64{0, 5e-324}, []float64{0, 1}},
		{"narrow and negative", []float64{-1e-323, -5e-324, 0}, []float64{0, 0.5, 1}},
	} {
		got := buildSpark(&sparkView{Label: "x", Points: c.points})
		if !reflect.DeepEqual(got.Points, c.want) {
			t.Errorf("%s: points = %v, want %v", c.name, got.Points, c.want)
		}
		if _, err := json.Marshal(got); err != nil {
			t.Errorf("%s: does not marshal: %v", c.name, err)
		}
	}
	for v, want := range map[float64]string{12480.5: "12480.5", 0.1 + 0.2: "0.3", -0.001: "0", 100: "100", -2.25: "-2.25"} {
		if got := number(v); got != want {
			t.Errorf("number(%v) = %q, want %q", v, got, want)
		}
	}
}

func refsOf(t *testing.T) (water, bank ItemRef) {
	t.Helper()
	b := buildScreen(t, screenJSON)
	return b.Refs["demo.tasks t:41"], b.Refs["demo.tasks t:42"]
}

func TestAnActionPostsTheKeyAndTheScreen(t *testing.T) {
	water, _ := refsOf(t)
	c := &fakeClient{post: func(string, any) (string, error) {
		return `{"ok":true,"message":"Ticked","refresh":true,"outcome":"done"}`, nil
	}}
	res, err := WidgetsSource{}.Act(context.Background(), c, "tick", water, nil)
	if err != nil || !res.Refetch || res.Message != "Ticked" || res.Raw != nil {
		t.Fatalf("Act = %+v, %v", res, err)
	}
	if len(c.posts) != 1 || c.posts[0].path != "/widgets/demo.tasks/actions/tick" ||
		c.posts[0].body != `{"key":"t:41","screen":"tablet"}` {
		t.Fatalf("posts = %+v", c.posts)
	}
	c.post = func(string, any) (string, error) { return `{"ok":true,"refresh":false,"outcome":"done"}`, nil }
	if res, err := (WidgetsSource{}).Act(context.Background(), c, "tick", water, nil); err != nil || res.Refetch || res.Message != "Done" {
		t.Fatalf("quiet done = %+v, %v", res, err)
	}
}

func TestAnActionTheItemDoesNotOfferIsNeverSent(t *testing.T) {
	water, _ := refsOf(t)
	c := &fakeClient{post: func(string, any) (string, error) { return `{"ok":true}`, nil }}
	for _, action := range []string{"purge", "snooze", "archive"} {
		if _, err := (WidgetsSource{}).Act(context.Background(), c, action, water, nil); err == nil ||
			err.Error() != "this item doesn't offer that action" {
			t.Fatalf("%s: err = %v", action, err)
		}
	}
	if len(c.posts) != 0 {
		t.Fatalf("sent %d requests", len(c.posts))
	}
}

func TestActionOutcomes(t *testing.T) {
	water, _ := refsOf(t)
	for _, tc := range []struct {
		name    string
		err     error
		body    string
		refetch bool
		want    string
	}{
		{"refused", &HTTPError{Status: 409, Message: "no suggested transaction", Outcome: "refused"}, "", true, "no suggested transaction"},
		{"item gone", &HTTPError{Status: 409, Message: "item gone — refresh", Outcome: "refused"}, "", true, "item gone — refresh"},
		{"refused, no reason", &HTTPError{Status: 409}, "", true, "The app refused this"},
		{"200 but not ok", nil, `{"ok":false,"message":"nothing to do","outcome":"refused"}`, true, "nothing to do"},
		{"denied by the layout", &HTTPError{Status: 403, Message: "not on this screen", Outcome: "denied"}, "", false, "not on this screen"},
		// The gate answers a widget action in the action's own shape, so its
		// refusal carries outcome "denied" too; it keeps the default wording.
		{"refused by the gate", &HTTPError{Status: 403, Message: "peer not allowed", Outcome: "denied"}, "", false,
			"The Mac doesn't recognise this tablet yet. Try again in a minute."},
		{"refused by the gate, grant", &HTTPError{Status: 403, Message: "service peer not permitted", Outcome: "denied"}, "", false,
			"Tablet not authorised. Run rm-today-setup."},
		{"refused without an outcome", &HTTPError{Status: 403, Message: "service-peer-route"}, "", false, "Tablet not authorised. Run rm-today-setup."},
		{"unknown action", &HTTPError{Status: 404, Message: "unknown action", Outcome: "denied"}, "", true, "unknown action"},
		{"the app timed out", &HTTPError{Status: 504, Message: "Unknown — check in Demo", Outcome: "unknown"}, "", false, "Unknown — check in Demo"},
		{"the proxy gave up", &HTTPError{Status: 502}, "", false, "Unknown — check Tasks in its app"},
		{"the tablet timed out", context.DeadlineExceeded, "", false, "Unknown — check Tasks in its app"},
		{"tailscale down", fmt.Errorf("%w (refused)", ErrTailscaleDown), "", false, "Tailscale isn't running on the tablet"},
		{"token refused", &HTTPError{Status: 401, Message: "bad token"}, "", false, "Tablet not authorised. Run rm-today-setup."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &fakeClient{post: func(string, any) (string, error) { return tc.body, tc.err }}
			res, err := WidgetsSource{}.Act(context.Background(), c, "tick", water, nil)
			if err == nil || UserMessage(err) != tc.want || res.Refetch != tc.refetch {
				t.Fatalf("Act = %+v, %v (%q); want refetch %v, %q", res, err, UserMessage(err), tc.refetch, tc.want)
			}
			if len(c.posts) != 1 {
				t.Fatalf("sent %d requests, want exactly 1", len(c.posts))
			}
		})
	}
}

// screenServer serves one screen, counts fetches and answers actions.
type screenServer struct {
	body  atomic.Value // string
	gets  atomic.Int32
	posts atomic.Int32
	post  func() (string, error)
}

func (s *screenServer) Get(_ context.Context, path string, out any) error {
	s.gets.Add(1)
	if path != "/screens/tablet" {
		return &HTTPError{Status: 404, Message: "no route"}
	}
	return json.Unmarshal([]byte(s.body.Load().(string)), out)
}

func (s *screenServer) Post(_ context.Context, _ string, _, out any) error {
	s.posts.Add(1)
	answer, err := s.post()
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(answer), out)
}

func widgetsEngine(t *testing.T, srv *screenServer) (*Engine, *recorder) {
	t.Helper()
	rec := newRecorder()
	return New(context.Background(), srv, []Source{WidgetsSource{}}, spLoc(t),
		filepath.Join(t.TempDir(), "today.json"), rec.emit, func() time.Time { return widgetsNow }), rec
}

func TestADoneActionRefetchesTheScreenOnce(t *testing.T) {
	srv := &screenServer{}
	srv.body.Store(screenJSON)
	srv.post = func() (string, error) {
		srv.body.Store(strings.Replace(screenJSON, waterItem, "", 1))
		return `{"ok":true,"message":"Done","refresh":true,"outcome":"done"}`, nil
	}
	e, rec := widgetsEngine(t, srv)
	e.Refresh()
	sec := find(rec.snapshot(t), "widgets")
	if sec.Status != "ok" || sec.Screen == nil || titlesOfCell(sec.Screen.Cells[0]) != "Water the plants,Call the bank" {
		t.Fatalf("before = %+v", sec)
	}
	e.Act("widgets", sec.Rev, "tick", "demo.tasks t:41")
	if r := rec.result(t); !r.OK || r.Message != "Done" {
		t.Fatalf("result = %+v", r)
	}
	rec.snapshot(t) // the state after the action
	after := find(rec.snapshot(t), "widgets")
	if titlesOfCell(after.Screen.Cells[0]) != "Call the bank" || after.Rev == sec.Rev {
		t.Fatalf("after = %+v", after.Screen.Cells[0])
	}
	if srv.gets.Load() != 2 || srv.posts.Load() != 1 {
		t.Fatalf("gets %d posts %d, want 2 and 1", srv.gets.Load(), srv.posts.Load())
	}
}

func TestAnUnknownOutcomeIsNeverRetriedOrRefetched(t *testing.T) {
	srv := &screenServer{}
	srv.body.Store(screenJSON)
	srv.post = func() (string, error) {
		return "", &HTTPError{Status: 504, Message: "Unknown — check in Demo", Outcome: "unknown"}
	}
	e, rec := widgetsEngine(t, srv)
	e.Refresh()
	sec := find(rec.snapshot(t), "widgets")
	e.Act("widgets", sec.Rev, "tick", "demo.tasks t:41")
	if r := rec.result(t); r.OK || r.Message != "Unknown — check in Demo" {
		t.Fatalf("result = %+v", r)
	}
	rec.snapshot(t)
	time.Sleep(50 * time.Millisecond)
	if srv.gets.Load() != 1 || srv.posts.Load() != 1 {
		t.Fatalf("gets %d posts %d, want 1 and 1", srv.gets.Load(), srv.posts.Load())
	}
}

func titlesOfCell(c Cell) string {
	var out []string
	for _, g := range c.Groups {
		for _, it := range g.Items {
			out = append(out, it.Title)
		}
	}
	return strings.Join(out, ",")
}

// metricsCell lays one metrics widget out and returns its cell, whose data
// is the "metrics" value given.
func metricsCell(t *testing.T, metrics string) Cell {
	t.Helper()
	s := screenOf(t, `{"layout":{"grid":{"cols":2},"items":[{"widget":"demo.m","view":"metrics","x":0,"y":0,"w":2,"h":2}]},
	 "widgets":{"demo.m":{"title":"Money","state":"ok","data":{"id":"m","title":"Money","as_of":"2026-10-07T09:00:00-03:00",
	  "metrics":`+metrics+`},"actions":[]}}}`)
	if len(s.Cells) != 1 {
		t.Fatalf("cells = %+v", s.Cells)
	}
	return s.Cells[0]
}

func TestMetricsRowsKeepTheirOrderAndFields(t *testing.T) {
	c := metricsCell(t, `{"rows":[
	 {"label":"Realized P&L","value":"+$12.10","detail":"22 closed trades · live epochs","tone":"good"},
	 {"label":"Open risk","value":"3","tone":"loud"},
	 {"label":"Bots","value":"2 / 4","detail":"","tone":"bad"}]}`)
	if c.Problem || c.Note != "" || viewsSet(c) != 1 || c.Metrics == nil {
		t.Fatalf("cell = %+v", c)
	}
	want := []MetricRow{
		{Label: "Realized P&L", Value: "+$12.10", Detail: "22 closed trades · live epochs", Tone: "good"},
		{Label: "Open risk", Value: "3", Tone: "neutral"},
		{Label: "Bots", Value: "2 / 4", Tone: "bad"},
	}
	if !reflect.DeepEqual(c.Metrics.Rows, want) {
		t.Fatalf("rows\n got %+v\nwant %+v", c.Metrics.Rows, want)
	}
	// What the view receives.
	if got := mustJSON(c.Metrics); got != `{"rows":[{"label":"Realized P\u0026L","value":"+$12.10","detail":"22 closed trades · live epochs","tone":"good"},`+
		`{"label":"Open risk","value":"3","tone":"neutral"},{"label":"Bots","value":"2 / 4","tone":"bad"}]}` {
		t.Fatalf("json = %s", got)
	}
}

func TestMetricsRowsAreBounded(t *testing.T) {
	var rows []string
	for i := 1; i <= 10; i++ {
		rows = append(rows, fmt.Sprintf(`{"label":"L%d","value":"V%d"}`, i, i))
	}
	c := metricsCell(t, `{"rows":[`+strings.Join(rows, ",")+`]}`)
	if len(c.Metrics.Rows) != 8 || c.Metrics.Rows[7].Label != "L8" {
		t.Fatalf("rows = %+v", c.Metrics.Rows)
	}

	// A row without a label or a value has nothing to show; the rest stay.
	c = metricsCell(t, `{"rows":[{"label":"","value":"1"},{"label":"A","value":""},{"label":"B","value":"2"},{"value":"3"}]}`)
	if len(c.Metrics.Rows) != 1 || c.Metrics.Rows[0].Label != "B" {
		t.Fatalf("rows = %+v", c.Metrics.Rows)
	}

	long := strings.Repeat("é", 501)
	c = metricsCell(t, `{"rows":[{"label":"`+long+`","value":"`+long+`","detail":"`+long+`"}]}`)
	r := c.Metrics.Rows[0]
	for _, s := range []string{r.Label, r.Value, r.Detail} {
		if len([]rune(s)) != 500 {
			t.Fatalf("a string kept %d characters", len([]rune(s)))
		}
	}
}

func TestMetricsWithNoUsableRowDrawsEmpty(t *testing.T) {
	for _, m := range []string{`{"rows":[]}`, `{"rows":[{"label":"","value":""}]}`, `{}`, `null`} {
		c := metricsCell(t, m)
		if c.Metrics != nil || c.Problem || viewsSet(c) != 0 {
			t.Errorf("%s: cell = %+v", m, c)
		}
	}
}

func TestMetricsOfTheWrongTypeFailOnlyThatCell(t *testing.T) {
	s := screenOf(t, `{"layout":{"grid":{"cols":2},"items":[
	 {"widget":"demo.bad","view":"metrics","x":0,"y":0,"w":1,"h":1},
	 {"widget":"demo.ok","view":"metrics","x":1,"y":0,"w":1,"h":1}]},
	 "widgets":{
	  "demo.bad":{"title":"Bad","state":"ok","data":{"id":"b","title":"Bad","as_of":"2026-10-07T09:00:00-03:00","metrics":{"rows":[{"label":"A","value":5}]}},"actions":[]},
	  "demo.ok":{"title":"Ok","state":"ok","data":{"id":"o","title":"Ok","as_of":"2026-10-07T09:00:00-03:00","metrics":{"rows":[{"label":"A","value":"5"}]}},"actions":[]}}}`)
	if !s.Cells[0].Problem || s.Cells[0].Note != "unavailable: this tablet can't read its data" || s.Cells[1].Metrics == nil {
		t.Fatalf("cells = %+v", s.Cells)
	}
}

func TestATapActionPostsTheKeyAndTheTappedScreen(t *testing.T) {
	c := &fakeClient{post: func(string, any) (string, error) {
		return `{"ok":true,"message":"Paid","refresh":true,"outcome":"done"}`, nil
	}}
	got := TapAct(context.Background(), c, "vault.money", "tmpl:abc def", "confirm_payment", "page2", "Rent")
	if got != (TapResult{OK: true, Message: "Paid", Outcome: "done", Refresh: true}) {
		t.Fatalf("TapAct = %+v", got)
	}
	if len(c.posts) != 1 || c.posts[0].path != "/widgets/vault.money/actions/confirm_payment" ||
		c.posts[0].body != `{"key":"tmpl:abc def","screen":"page2"}` {
		t.Fatalf("posts = %+v", c.posts)
	}
	// Each id is one path segment, whatever it holds.
	TapAct(context.Background(), c, "a.b/../c", "k", "x/y", "page1", "")
	if c.posts[1].path != "/widgets/a.b%2F..%2Fc/actions/x%2Fy" {
		t.Fatalf("path = %s", c.posts[1].path)
	}
	// A quiet success.
	c.post = func(string, any) (string, error) { return `{"ok":true}`, nil }
	if got := TapAct(context.Background(), c, "vault.money", "k", "a", "page1", ""); got != (TapResult{OK: true, Message: "Done", Outcome: "ok"}) {
		t.Fatalf("quiet success = %+v", got)
	}
}

// A tap action fails the way Act does, with the outcome the gateway named.
func TestTapActionOutcomesMatchActs(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		body string
		want TapResult
	}{
		{"refused", &HTTPError{Status: 409, Message: "no suggested transaction", Outcome: "refused"}, "",
			TapResult{Message: "no suggested transaction", Outcome: "refused", Refresh: true}},
		{"refused, no outcome", &HTTPError{Status: 409}, "", TapResult{Message: "The app refused this", Outcome: "refused", Refresh: true}},
		{"200 but not ok", nil, `{"ok":false,"message":"nothing to do"}`, TapResult{Message: "nothing to do", Outcome: "refused", Refresh: true}},
		{"denied by the layout", &HTTPError{Status: 403, Message: "not on this screen", Outcome: "denied"}, "",
			TapResult{Message: "not on this screen", Outcome: "denied"}},
		{"unknown action", &HTTPError{Status: 404, Message: "unknown action", Outcome: "denied"}, "",
			TapResult{Message: "unknown action", Outcome: "denied", Refresh: true}},
		{"the app timed out", &HTTPError{Status: 504, Message: "Unknown — check in Demo", Outcome: "unknown"}, "",
			TapResult{Message: "Unknown — check in Demo", Outcome: "unknown"}},
		{"the proxy gave up", &HTTPError{Status: 502}, "", TapResult{Message: "Unknown — check Rent in its app", Outcome: "unknown"}},
		{"the tablet timed out", context.DeadlineExceeded, "", TapResult{Message: "Unknown — check Rent in its app", Outcome: "unknown"}},
		{"tailscale down", fmt.Errorf("%w (refused)", ErrTailscaleDown), "", TapResult{Message: "Tailscale isn't running on the tablet", Outcome: "error"}},
		{"token refused", &HTTPError{Status: 401, Message: "bad token"}, "", TapResult{Message: "Tablet not authorised. Run rm-today-setup.", Outcome: "error"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &fakeClient{post: func(string, any) (string, error) { return tc.body, tc.err }}
			got := TapAct(context.Background(), c, "vault.money", "k", "a", "page1", "Rent")
			if got != tc.want {
				t.Fatalf("TapAct = %+v, want %+v", got, tc.want)
			}
			if len(c.posts) != 1 {
				t.Fatalf("sent %d requests, want exactly 1", len(c.posts))
			}
		})
	}
	// With no title the widget id names the item.
	c := &fakeClient{post: func(string, any) (string, error) { return "", context.DeadlineExceeded }}
	if got := TapAct(context.Background(), c, "vault.money", "k", "a", "page1", ""); got.Message != "Unknown — check vault.money in its app" {
		t.Fatalf("message = %q", got.Message)
	}
}
