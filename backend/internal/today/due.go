package today

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// maxTickText is the longest item text the gateway will tick, in characters
// (not bytes). A longer item is shown, but without a Done action.
const maxTickText = 300

// DueSource is the operator's dated personal items, overdue through the next
// 7 days, with one action: tick.
type DueSource struct{}

func (DueSource) ID() string           { return "due" }
func (DueSource) Title() string        { return "Due" }
func (DueSource) Placement() Placement { return Column }

type personalItem struct {
	Text    string `json:"text"`
	Due     string `json:"due"`
	Section string `json:"section"`
}

type personalFile struct {
	Name  string         `json:"name"`
	Title string         `json:"title"`
	Open  []personalItem `json:"open"`
}

type personalSnapshot struct {
	Exists *bool          `json:"exists"`
	Files  []personalFile `json:"files"`
}

// Fetch keeps only what Today uses of GET /personal.
func (DueSource) Fetch(ctx context.Context, c Client) (json.RawMessage, error) {
	var s personalSnapshot
	if err := c.Get(ctx, "/personal", &s); err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

func (DueSource) Build(raw json.RawMessage, now time.Time, loc *time.Location) (Built, error) {
	var s personalSnapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return Built{}, err
	}
	if s.Exists != nil && !*s.Exists {
		return Built{}, errors.New("the personal folder can't be read on the Mac")
	}
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	horizon := today.AddDate(0, 0, 8) // the window: overdue through day +7
	type entry struct {
		due  time.Time
		item Item
	}
	var overdue, todays, week []entry
	b := Built{Refs: map[string]ItemRef{}}
	for _, f := range s.Files {
		label := f.Title
		if label == "" {
			label = strings.TrimSuffix(f.Name, ".md")
		}
		for i, o := range f.Open {
			if o.Due == "" {
				continue
			}
			d, err := time.ParseInLocation(dateLayout, o.Due, loc)
			if err != nil || !d.Before(horizon) {
				continue
			}
			key := fmt.Sprintf("due:%s#%d", f.Name, i)
			b.Refs[key] = ItemRef{"file": f.Name, "text": o.Text}
			it := Item{
				Key: key, Title: plain(o.Text), Detail: o.Text,
				Subtitle: d.Format("Mon 2 Jan") + " · " + label,
			}
			if utf8.RuneCountInString(o.Text) <= maxTickText {
				it.Actions = []Action{{ID: "tick", Label: "✓ Done"}}
			}
			e := entry{d, it}
			switch {
			case d.Before(today):
				overdue = append(overdue, e)
			case d.Equal(today):
				todays = append(todays, e)
			default:
				week = append(week, e)
			}
		}
	}
	for _, g := range []struct {
		title string
		es    []entry
	}{{"Overdue", overdue}, {"Today", todays}, {"Next 7 days", week}} {
		if len(g.es) == 0 {
			continue
		}
		sort.SliceStable(g.es, func(i, j int) bool { return g.es[i].due.Before(g.es[j].due) })
		grp := Group{Title: g.title}
		for _, e := range g.es {
			grp.Items = append(grp.Items, e.item)
		}
		b.Groups = append(b.Groups, grp)
	}
	return b, nil
}

// Act ticks one item: POST /personal/items with the gateway's own text. The
// answer carries the file as it now is, which replaces it in the section.
func (DueSource) Act(ctx context.Context, c Client, action string, ref ItemRef, raw json.RawMessage) (ActResult, error) {
	if action != "tick" {
		return ActResult{}, fmt.Errorf("unknown action %q", action)
	}
	var resp struct {
		File personalFile `json:"file"`
	}
	err := c.Post(ctx, "/personal/items", map[string]string{"op": "tick", "file": ref["file"], "text": ref["text"]}, &resp)
	var he *HTTPError
	if errors.As(err, &he) && he.Status == http.StatusConflict {
		// No match, more than one, or the file changed: the list is stale.
		return ActResult{Refetch: true}, err
	}
	if err != nil {
		return ActResult{}, err
	}
	var s personalSnapshot
	if json.Unmarshal(raw, &s) != nil {
		return ActResult{Refetch: true, Message: "Done"}, nil
	}
	for i := range s.Files {
		if s.Files[i].Name == resp.File.Name {
			s.Files[i] = resp.File
			nb, err := json.Marshal(s)
			if err != nil {
				return ActResult{Refetch: true, Message: "Done"}, nil
			}
			return ActResult{Raw: nb, Message: "Done"}, nil
		}
	}
	return ActResult{Refetch: true, Message: "Done"}, nil
}
