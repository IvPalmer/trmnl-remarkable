package today

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Placement is where a section is drawn: Banner across the top, Column beside
// the other columns.
type Placement string

const (
	Banner Placement = "banner"
	Column Placement = "column"
)

// Action is one button on an item's sheet.
type Action struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Item is one row. Key is opaque to the QML; the backend maps it to an ItemRef.
type Item struct {
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	Actions  []Action `json:"actions,omitempty"`
}

type Group struct {
	Title string `json:"title"`
	Items []Item `json:"items"`
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
}

// ActResult is what an action changed. Raw, when set, replaces the section's
// data and its cache; Refetch asks for one fresh fetch of the section.
type ActResult struct {
	Raw     json.RawMessage
	Refetch bool
	Message string
}

var ErrNoActions = errors.New("this section has no actions")

// Source is one section of Today. To add one: implement this in its own file,
// add it to Registry, and allow its route in the gateway's grant for the
// tablet if the route is new. The QML needs no change.
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
	return []Source{BriefSource{}}
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

// plain drops the bold and code marks the gateway's brief lines carry.
func plain(s string) string { return strings.NewReplacer("**", "", "`", "").Replace(s) }
