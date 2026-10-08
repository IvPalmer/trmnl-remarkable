package today

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The screen this tablet shows, and the grid's height limit.
const (
	screenName = "tablet"
	maxRows    = 24
)

var knownViews = map[string]bool{"stat": true, "list": true, "spark": true, "alert": true, "metrics": true}

// WidgetsSource is the whole Today screen: the gateway's layout for this
// screen and the data of every widget placed on it. Widgets and the apps
// behind them arrive at runtime; nothing here names one.
type WidgetsSource struct{}

func (WidgetsSource) ID() string           { return "widgets" }
func (WidgetsSource) Title() string        { return "Today" }
func (WidgetsSource) Placement() Placement { return Column }

// What GET /screens/tablet carries that the tablet uses. Fetch writes the
// same shape back, so it is also what the cache holds.
type screenDoc struct {
	Layout  layoutDoc              `json:"layout"`
	Warning string                 `json:"layout_warning,omitempty"`
	Widgets map[string]widgetEntry `json:"widgets"`
}

type layoutDoc struct {
	Grid struct {
		Cols int `json:"cols"`
	} `json:"grid"`
	Items []placement `json:"items"`
}

type placement struct {
	Widget string `json:"widget"`
	View   string `json:"view"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	W      int    `json:"w"`
	H      int    `json:"h"`
}

type widgetEntry struct {
	Title   string          `json:"title"`
	State   string          `json:"state"`
	Error   string          `json:"error,omitempty"`
	Data    *widgetData     `json:"data,omitempty"`
	Actions []catalogAction `json:"actions"`
}

type catalogAction struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Risk    string `json:"risk"`
	Confirm string `json:"confirm,omitempty"`
}

type widgetData struct {
	AsOf    string       `json:"as_of"`
	Tone    string       `json:"tone,omitempty"`
	Stat    *statView    `json:"stat,omitempty"`
	List    *listView    `json:"list,omitempty"`
	Spark   *sparkView   `json:"spark,omitempty"`
	Alert   *Alert       `json:"alert,omitempty"`
	Metrics *metricsView `json:"metrics,omitempty"`
}

type statView struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Delta string `json:"delta,omitempty"`
	Tone  string `json:"tone,omitempty"`
}

type listView struct {
	Groups []listGroup `json:"groups"`
}

type listGroup struct {
	Title string     `json:"title"`
	Items []listItem `json:"items"`
}

type listItem struct {
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	Tone     string   `json:"tone,omitempty"`
	Actions  []string `json:"actions,omitempty"`
}

type metricsView struct {
	Rows []metricRow `json:"rows"`
}

type metricRow struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Detail string `json:"detail,omitempty"`
	Tone   string `json:"tone,omitempty"`
}

type sparkView struct {
	Label  string    `json:"label"`
	Points []float64 `json:"points"`
	Unit   string    `json:"unit,omitempty"`
}

// Fetch keeps what the screen shows. Each widget entry is read on its own:
// one this tablet can't read becomes an error entry, so it never takes the
// rest of the screen with it.
func (WidgetsSource) Fetch(ctx context.Context, c Client) (json.RawMessage, error) {
	var wire struct {
		Layout  layoutDoc                  `json:"layout"`
		Warning string                     `json:"layout_warning"`
		Widgets map[string]json.RawMessage `json:"widgets"`
	}
	if err := c.Get(ctx, "/screens/"+screenName, &wire); err != nil {
		return nil, err
	}
	doc := screenDoc{Layout: wire.Layout, Warning: wire.Warning, Widgets: map[string]widgetEntry{}}
	for id, raw := range wire.Widgets {
		var w widgetEntry
		if json.Unmarshal(raw, &w) != nil {
			w = widgetEntry{State: "error", Error: "unavailable: this tablet can't read its data"}
		}
		doc.Widgets[id] = w
	}
	return json.Marshal(doc)
}

// Build lays the screen out as of now in loc: alerts become banners, the
// other placements cells in reading order, and rows no cell uses close up.
// A placement the grid can't hold is left out, with a warning.
func (WidgetsSource) Build(raw json.RawMessage, now time.Time, loc *time.Location) (Built, error) {
	var doc screenDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Built{}, err
	}
	if loc == nil {
		loc = time.UTC
	}
	cols := doc.Layout.Grid.Cols
	if cols < 1 {
		cols = 2
	}
	items := append([]placement(nil), doc.Layout.Items...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Y != items[j].Y {
			return items[i].Y < items[j].Y
		}
		return items[i].X < items[j].X
	})
	b := Built{Refs: map[string]ItemRef{}}
	s := &Screen{Cols: cols, Banners: []Cell{}, Cells: []Cell{}}
	outside := 0
	for _, p := range items {
		if p.W < 1 || p.H < 1 || p.X < 0 || p.Y < 0 || p.X+p.W > cols || p.Y+p.H > maxRows {
			outside++
			continue
		}
		c := buildCell(p, doc.Widgets, now, loc, b.Refs)
		switch {
		case p.View != "alert":
			s.Cells = append(s.Cells, c)
		case c.Alert != nil || c.Problem:
			s.Banners = append(s.Banners, c) // an alert with nothing to say takes no space
		}
	}
	s.Rows = closeRows(s.Cells)
	var warnings []string
	if doc.Warning != "" {
		warnings = append(warnings, doc.Warning)
	}
	switch {
	case outside == 1:
		warnings = append(warnings, "1 widget outside the grid was left out")
	case outside > 1:
		warnings = append(warnings, fmt.Sprintf("%d widgets outside the grid were left out", outside))
	}
	b.Warning = strings.Join(warnings, " · ")
	b.Screen = s
	return b, nil
}

func buildCell(p placement, widgets map[string]widgetEntry, now time.Time, loc *time.Location, refs map[string]ItemRef) Cell {
	c := Cell{Widget: p.Widget, View: p.View, X: p.X, Y: p.Y, W: p.W, H: p.H, Title: p.Widget, Tone: "neutral"}
	w, ok := widgets[p.Widget]
	if !ok {
		return unavailable(c, "unavailable: no data for this widget")
	}
	if w.Title != "" {
		c.Title = w.Title
	}
	if !knownViews[p.View] {
		return unavailable(c, fmt.Sprintf("unavailable: this tablet can't show the %q view", p.View))
	}
	switch w.State {
	case "ok":
	case "stale":
		c.Note = staleNote(w.Data, now, loc)
	default: // "error", or a state this tablet doesn't know
		switch msg := w.Error; {
		case msg == "":
			return unavailable(c, "unavailable")
		case strings.HasPrefix(msg, "unavailable"):
			return unavailable(c, msg)
		default:
			return unavailable(c, "unavailable: "+msg)
		}
	}
	d := w.Data
	if d == nil {
		return c // a view the payload leaves out renders empty
	}
	c.Tone = tone(d.Tone)
	switch p.View {
	case "stat":
		c.Stat = buildStat(d.Stat)
	case "list":
		c.Groups = buildList(p.Widget, c.Title, d.List, w.Actions, refs)
	case "spark":
		c.Spark = buildSpark(d.Spark)
	case "alert":
		if d.Alert != nil {
			c.Alert = &Alert{Text: d.Alert.Text, Tone: tone(d.Alert.Tone)}
		}
	case "metrics":
		c.Metrics = buildMetrics(d.Metrics)
	}
	return c
}

func unavailable(c Cell, msg string) Cell {
	c.Note, c.Problem = msg, true
	return c
}

// staleNote is what a widget served from the gateway's cache says: its own
// as_of, which stays true, and "offline".
func staleNote(d *widgetData, now time.Time, loc *time.Location) string {
	if d != nil {
		if at, err := time.Parse(time.RFC3339, d.AsOf); err == nil {
			return "as of " + asOfText(at, now, loc) + " · " + offline
		}
	}
	return offline
}

// tone is one of the four the view knows; anything else is neutral.
func tone(t string) string {
	switch t {
	case "good", "warn", "bad":
		return t
	}
	return "neutral"
}

func buildStat(v *statView) *Stat {
	if v == nil {
		return nil
	}
	s := &Stat{Value: v.Value, Label: v.Label, Delta: v.Delta, Tone: tone(v.Tone)}
	switch d := strings.TrimSpace(v.Delta); {
	case strings.HasPrefix(d, "+"):
		s.Trend = "up"
	case strings.HasPrefix(d, "-"), strings.HasPrefix(d, "−"):
		s.Trend = "down"
	}
	return s
}

// The metrics contract: one to eight rows, each with a label and a value, and
// strings of at most 500 characters.
const (
	maxMetricRows = 8
	maxMetricStr  = 500
)

// buildMetrics keeps the rows the contract allows, in order: a row without a
// label or a value has nothing to show and is left out, and no more than eight
// are kept. A view with no row left is no view.
func buildMetrics(v *metricsView) *Metrics {
	if v == nil {
		return nil
	}
	m := &Metrics{Rows: []MetricRow{}}
	for _, r := range v.Rows {
		if r.Label == "" || r.Value == "" {
			continue
		}
		if len(m.Rows) == maxMetricRows {
			break
		}
		m.Rows = append(m.Rows, MetricRow{Label: clip(r.Label), Value: clip(r.Value), Detail: clip(r.Detail), Tone: tone(r.Tone)})
	}
	if len(m.Rows) == 0 {
		return nil
	}
	return m
}

// clip cuts s to maxMetricStr characters.
func clip(s string) string {
	if r := []rune(s); len(r) > maxMetricStr {
		return string(r[:maxMetricStr])
	}
	return s
}

// buildList keys each item "<widget> <key>" (the app's key is stable) and
// offers only the actions the catalog lists for this tablet at low or
// medium risk, whatever the item or the gateway says.
func buildList(wid, title string, v *listView, catalog []catalogAction, refs map[string]ItemRef) []Group {
	if v == nil {
		return nil
	}
	offered := map[string]catalogAction{}
	for _, a := range catalog {
		if a.Risk == "low" || a.Risk == "medium" {
			offered[a.ID] = a
		}
	}
	groups := []Group{}
	for _, g := range v.Groups {
		grp := Group{Title: g.Title, Items: []Item{}}
		for _, it := range g.Items {
			key := wid + " " + it.Key
			item := Item{Key: key, Title: it.Title, Subtitle: it.Subtitle, Detail: it.Detail, Tone: tone(it.Tone)}
			var ids []string
			for _, id := range it.Actions {
				a, ok := offered[id]
				if !ok {
					continue
				}
				item.Actions = append(item.Actions, Action{ID: a.ID, Label: a.Label, Risk: a.Risk, Confirm: confirmText(a, it.Title)})
				ids = append(ids, a.ID)
			}
			refs[key] = ItemRef{"widget": wid, "key": it.Key, "title": title, "actions": strings.Join(ids, " ")}
			grp.Items = append(grp.Items, item)
		}
		groups = append(groups, grp)
	}
	return groups
}

// confirmText is the question a medium action asks, in the catalog's words.
func confirmText(a catalogAction, title string) string {
	switch {
	case a.Risk != "medium":
		return ""
	case a.Confirm != "":
		return strings.ReplaceAll(a.Confirm, "{title}", title)
	default:
		return a.Label + ": " + title + "?"
	}
}

// buildSpark scales the points to 0..1; a flat line sits in the middle.
func buildSpark(v *sparkView) *Spark {
	if v == nil || len(v.Points) < 2 {
		return nil
	}
	lo, hi := v.Points[0], v.Points[0]
	for _, p := range v.Points {
		lo, hi = math.Min(lo, p), math.Max(hi, p)
	}
	s := &Spark{Label: v.Label, Unit: v.Unit, Min: number(lo), Max: number(hi),
		Last: number(v.Points[len(v.Points)-1]), Points: make([]float64, 0, len(v.Points))}
	// Scaled directly. Only when hi-lo overflows to +Inf (finite values far
	// apart) are the points halved first, which would underflow a tiny span.
	span := hi - lo
	for _, p := range v.Points {
		y := 0.5
		switch {
		case math.IsInf(span, 1):
			y = (p/2 - lo/2) / (hi/2 - lo/2)
		case span > 0:
			y = (p - lo) / span
		}
		// A NaN or Inf point would fail json.Marshal and blank the whole screen.
		if math.IsNaN(y) || math.IsInf(y, 0) {
			y = 0.5
		}
		s.Points = append(s.Points, y)
	}
	return s
}

// number writes v with at most two decimals and no trailing zeros.
func number(v float64) string {
	s := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 2, 64), "0"), ".")
	if s == "-0" {
		return "0"
	}
	return s
}

// closeRows renumbers the rows so that none is empty: an alert is drawn as
// a banner, and a gap draws nothing. A cell's rows are all in use, so it
// keeps its height. It returns how many rows are left.
func closeRows(cells []Cell) int {
	used := map[int]bool{}
	for _, c := range cells {
		for y := c.Y; y < c.Y+c.H; y++ {
			used[y] = true
		}
	}
	at, n := map[int]int{}, 0
	for y := 0; y < maxRows; y++ {
		if used[y] {
			at[y] = n
			n++
		}
	}
	for i := range cells {
		cells[i].Y = at[cells[i].Y]
	}
	return n
}

// Act runs one action through the gateway, which checks it against the
// tablet's grant, its layout and the action's risk before the app sees it.
// The answer carries no data: an action that changed something asks for one
// refetch. Nothing is ever sent twice.
func (WidgetsSource) Act(ctx context.Context, c Client, action string, ref ItemRef, _ json.RawMessage) (ActResult, error) {
	if !offers(ref["actions"], action) {
		return ActResult{}, errors.New("this item doesn't offer that action")
	}
	res, _, err := postAction(ctx, c, ref["widget"], action, ref["key"], screenName, ref["title"])
	return res, err
}

// postAction is the gateway call behind every action, whichever screen it
// comes from. The second result is the gateway's own word for what happened
// ("refused", "denied", "unknown"), or one worked out from a failure that
// carried none.
func postAction(ctx context.Context, c Client, widget, action, key, screen, title string) (ActResult, string, error) {
	var resp struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
		Refresh bool   `json:"refresh"`
		Outcome string `json:"outcome"`
	}
	path := "/widgets/" + url.PathEscape(widget) + "/actions/" + url.PathEscape(action)
	if err := c.Post(ctx, path, map[string]string{"key": key, "screen": screen}, &resp); err != nil {
		res, err2 := actFailed(err, title)
		return res, failureOutcome(err), err2
	}
	if !resp.OK {
		return ActResult{Refetch: true}, orElse(resp.Outcome, "refused"), errors.New(orElse(resp.Message, "The app refused this"))
	}
	return ActResult{Refetch: resp.Refresh, Message: orElse(resp.Message, "Done")}, orElse(resp.Outcome, "ok"), nil
}

// failureOutcome is the outcome of a call that failed: the gateway's, when its
// answer named one; "unknown" when the action may have run; "error" when it
// never left the tablet or was turned away without one.
func failureOutcome(err error) string {
	var he *HTTPError
	switch {
	case errors.Is(err, ErrTailscaleDown):
		return "error"
	case !errors.As(err, &he):
		return "unknown"
	case he.Outcome != "":
		return he.Outcome
	case he.Status == http.StatusConflict:
		return "refused"
	case he.Status == http.StatusForbidden:
		return "denied"
	case he.Status >= 502 && he.Status <= 504:
		return "unknown"
	default:
		return "error"
	}
}

// TapResult is how a tap on the BYOS image's region went: message 111.
// Refresh means the screen the tap came from is out of date.
type TapResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Outcome string `json:"outcome"`
	Refresh bool   `json:"refresh"`
}

// TapAct runs an action chosen on a tap region of the server-rendered image.
// It is Act for that screen: the same gateway call, the same wording for a
// refusal, a denial or an unknown outcome, and nothing sent twice. The gateway
// is what checks the widget, key and screen; this only builds the call, so the
// caller validates the ids first. title names the item in an "unknown" answer
// (the widget id when empty).
func TapAct(ctx context.Context, c Client, widget, key, action, screen, title string) TapResult {
	res, outcome, err := postAction(ctx, c, widget, action, key, screen, orElse(title, widget))
	if err != nil {
		return TapResult{OK: false, Message: UserMessage(err), Outcome: outcome, Refresh: res.Refetch}
	}
	return TapResult{OK: true, Message: res.Message, Outcome: outcome, Refresh: res.Refetch}
}

// actFailed words a failed action (spec Appendix A3). A refusal (409) or an
// unknown widget or action (404) means the screen is stale: one refetch. A
// denial (403 with outcome "denied") is said as the gateway says it, except
// the gate's own refusals ("peer not allowed", "service peer not permitted"),
// which answer in the action's shape too and keep UserMessage's wording. A timeout or a dropped connection is
// "unknown": the action may have run, so nothing is retried or refetched. The
// tablet's own problems (Tailscale, its token) keep UserMessage's wording.
func actFailed(err error, title string) (ActResult, error) {
	unknown := "Unknown — check " + title + " in its app"
	var he *HTTPError
	switch {
	case errors.Is(err, ErrTailscaleDown):
		return ActResult{}, err // refused before it left the tablet
	case !errors.As(err, &he):
		return ActResult{}, errors.New(unknown)
	case he.Status == http.StatusConflict:
		return ActResult{Refetch: true}, errors.New(orElse(he.Message, "The app refused this"))
	case he.Status == http.StatusNotFound:
		return ActResult{Refetch: true}, errors.New(orElse(he.Message, "This action is no longer offered"))
	case he.Status == http.StatusForbidden && he.Outcome == "denied" &&
		he.Message != "peer not allowed" && he.Message != "service peer not permitted":
		return ActResult{}, errors.New(orElse(he.Message, "Not allowed from this tablet"))
	case he.Status >= 502 && he.Status <= 504:
		return ActResult{}, errors.New(orElse(he.Message, unknown))
	default:
		return ActResult{}, err
	}
}

func offers(ids, action string) bool {
	for _, id := range strings.Fields(ids) {
		if id == action {
			return true
		}
	}
	return false
}

func orElse(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
