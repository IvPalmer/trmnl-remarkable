package today

import (
	"context"
	"fmt"
	"reflect"
	"strings"
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
	for _, set := range []bool{c.Stat != nil, c.Groups != nil, c.Spark != nil, c.Alert != nil} {
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
	for v, want := range map[float64]string{12480.5: "12480.5", 0.1 + 0.2: "0.3", -0.001: "0", 100: "100", -2.25: "-2.25"} {
		if got := number(v); got != want {
			t.Errorf("number(%v) = %q, want %q", v, got, want)
		}
	}
}
