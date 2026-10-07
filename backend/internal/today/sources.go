package today

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Placement is where a section is drawn: Banner across the top, Column beside
// the other columns.
type Placement string

const (
	Banner Placement = "banner"
	Column Placement = "column"
)

// Action is one button on an item's sheet. Risk is the catalog's: the view
// shows only "low" and "medium", and a "medium" one asks Confirm first.
type Action struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Risk    string `json:"risk,omitempty"`
	Confirm string `json:"confirm,omitempty"`
}

// Item is one row. Key is opaque to the QML; the backend maps it to an ItemRef.
type Item struct {
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	Tone     string   `json:"tone,omitempty"`
	Actions  []Action `json:"actions,omitempty"`
}

type Group struct {
	Title string `json:"title"`
	Items []Item `json:"items"`
}

// Screen is a section drawn as a grid of widgets: Banners (the alert
// placements) across the top, then Cells on Cols columns and Rows rows.
// Cells are in reading order (y, then x), which is also the portrait order.
type Screen struct {
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	Banners []Cell `json:"banners"`
	Cells   []Cell `json:"cells"`
}

// Cell is one placement. Only its view's field is set. Note is the frame's
// status line ("as of 08:30 · offline", "unavailable: …"), empty when the
// data is fresh; Problem means the cell has nothing to draw but the note.
type Cell struct {
	Widget  string  `json:"widget"`
	View    string  `json:"view"`
	X       int     `json:"x"`
	Y       int     `json:"y"`
	W       int     `json:"w"`
	H       int     `json:"h"`
	Title   string  `json:"title"`
	Tone    string  `json:"tone"`
	Note    string  `json:"note,omitempty"`
	Problem bool    `json:"problem,omitempty"`
	Stat    *Stat   `json:"stat,omitempty"`
	Groups  []Group `json:"groups,omitempty"`
	Spark   *Spark  `json:"spark,omitempty"`
	Alert   *Alert  `json:"alert,omitempty"`
}

// Stat is a large number. Trend is "up" or "down" from Delta's sign.
type Stat struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Delta string `json:"delta,omitempty"`
	Trend string `json:"trend,omitempty"`
	Tone  string `json:"tone"`
}

// Spark is a line, oldest point first, each scaled to 0..1 (0 the lowest).
type Spark struct {
	Label  string    `json:"label"`
	Points []float64 `json:"points"`
	Min    string    `json:"min"`
	Max    string    `json:"max"`
	Last   string    `json:"last"`
	Unit   string    `json:"unit,omitempty"`
}

type Alert struct {
	Text string `json:"text"`
	Tone string `json:"tone"`
}

// Section is one source's part of message 109.
type Section struct {
	ID        string    `json:"id"`
	Rev       uint64    `json:"rev"`
	Title     string    `json:"title"`
	Placement Placement `json:"placement"`
	Status    string    `json:"status"` // "ok", "error" or "none"
	AsOf      string    `json:"as_of,omitempty"`
	Warning   string    `json:"warning,omitempty"`
	Error     string    `json:"error,omitempty"`
	Groups    []Group   `json:"groups"`
	Screen    *Screen   `json:"screen,omitempty"`
}

// ItemRef is what an action needs to find its item at the gateway. It never
// leaves the backend.
type ItemRef map[string]string

// Built is a section's content plus the private reference behind each key.
// Title, when set, is the heading the data brought with it and replaces the
// source's static Title; most sources leave it empty.
type Built struct {
	Title   string
	Groups  []Group
	Warning string
	Refs    map[string]ItemRef
	// Screen, when set, is the section drawn as a widget grid.
	Screen *Screen
}

// ActResult is what an action changed. Raw, when set, replaces the section's
// data and its cache; Refetch asks for one fresh fetch of the section.
type ActResult struct {
	Raw     json.RawMessage
	Refetch bool
	Message string
}

var ErrNoActions = errors.New("this section has no actions")

// Source is one section of Today. Today has one, widgets: the gateway's
// screen, whose widgets and apps arrive at runtime, so a new widget needs
// no change here. A test can register a fake Source; the QML draws a
// section's Screen.
type Source interface {
	ID() string
	// Title is the section's heading unless Build supplies one.
	Title() string
	Placement() Placement
	// Fetch gets the section's raw data from the gateway.
	Fetch(ctx context.Context, c Client) (json.RawMessage, error)
	// Build turns raw data into what is shown, as of now in loc. An error
	// means the data is unusable: the fetch failed, and nothing is cached.
	Build(raw json.RawMessage, now time.Time, loc *time.Location) (Built, error)
	// Act runs one action on one item; raw is the section's current data.
	Act(ctx context.Context, c Client, action string, ref ItemRef, raw json.RawMessage) (ActResult, error)
}

// Registry is every section Today knows, in the default order.
func Registry() []Source {
	return []Source{WidgetsSource{}}
}

// Enabled picks the sources named in order (all of Registry when order is
// empty), in that order. Names it does not know come back to be logged.
func Enabled(order []string) (sources []Source, unknown []string) {
	all := Registry()
	if len(order) == 0 {
		return all, nil
	}
	byID := map[string]Source{}
	for _, s := range all {
		byID[s.ID()] = s
	}
	seen := map[string]bool{}
	for _, id := range order {
		s, ok := byID[id]
		switch {
		case !ok:
			unknown = append(unknown, id)
		case !seen[id]:
			sources = append(sources, s)
			seen[id] = true
		}
	}
	return sources, unknown
}
