package today

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const dateLayout = "2006-01-02"

// BriefSource shows the latest daily brief across the top. Read-only.
type BriefSource struct{}

func (BriefSource) ID() string           { return "brief" }
func (BriefSource) Title() string        { return "Brief" }
func (BriefSource) Placement() Placement { return Banner }

type briefSnapshot struct {
	Brief *struct {
		Title    string `json:"title"`
		Created  string `json:"created"`
		IsToday  bool   `json:"is_today"`
		Sections []struct {
			Title string   `json:"title"`
			Lines []string `json:"lines"`
		} `json:"sections"`
	} `json:"brief"`
}

// Fetch keeps only what Today shows.
func (BriefSource) Fetch(ctx context.Context, c Client) (json.RawMessage, error) {
	var s briefSnapshot
	if err := c.Get(ctx, "/brief", &s); err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

// Build decides "today" from the brief's own date against now in loc, so a
// brief cached yesterday warns after midnight. The gateway's is_today, frozen
// when it was fetched, is only the fallback when created is not a date.
func (BriefSource) Build(raw json.RawMessage, now time.Time, loc *time.Location) (Built, error) {
	var s briefSnapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return Built{}, err
	}
	b := Built{Refs: map[string]ItemRef{}}
	if s.Brief == nil {
		return b, nil
	}
	// The section keeps the brief's own heading; Title() is only the fallback.
	b.Title = strings.TrimSpace(s.Brief.Title)
	if loc == nil {
		loc = time.UTC
	}
	isToday, created := s.Brief.IsToday, ""
	if d, err := time.Parse(dateLayout, s.Brief.Created); err == nil {
		created = d.Format(dateLayout)
		isToday = created == now.In(loc).Format(dateLayout)
	}
	if !isToday {
		b.Warning = "not today's brief"
		if created != "" {
			b.Warning += " (" + created + ")"
		}
	}
	for si, sec := range s.Brief.Sections {
		g := Group{Title: sec.Title, Items: []Item{}}
		for li, line := range sec.Lines {
			g.Items = append(g.Items, Item{Key: fmt.Sprintf("brief:%d:%d", si, li), Title: plain(line)})
		}
		b.Groups = append(b.Groups, g)
	}
	return b, nil
}

func (BriefSource) Act(context.Context, Client, string, ItemRef, json.RawMessage) (ActResult, error) {
	return ActResult{}, ErrNoActions
}
