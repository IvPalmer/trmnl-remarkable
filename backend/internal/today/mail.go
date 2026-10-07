package today

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MailSource is the important mail the gateway selects. Read-only.
type MailSource struct{}

func (MailSource) ID() string           { return "mail" }
func (MailSource) Title() string        { return "Mail" }
func (MailSource) Placement() Placement { return Column }

const mailShown = 10

type mailSnapshot struct {
	Accounts []struct {
		Email string `json:"email"`
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	} `json:"accounts"`
	Messages []struct {
		FromName string `json:"from_name"`
		FromAddr string `json:"from_addr"`
		Subject  string `json:"subject"`
		Snippet  string `json:"snippet"`
		Received string `json:"received"`
	} `json:"messages"`
}

// Fetch keeps only what Today shows: no message ids, thread ids or URLs reach
// the tablet's cache.
func (MailSource) Fetch(ctx context.Context, c Client) (json.RawMessage, error) {
	var s mailSnapshot
	if err := c.Get(ctx, "/mail", &s); err != nil {
		return nil, err
	}
	if len(s.Messages) > mailShown {
		s.Messages = s.Messages[:mailShown]
	}
	return json.Marshal(s)
}

func (MailSource) Build(raw json.RawMessage, _ time.Time, loc *time.Location) (Built, error) {
	var s mailSnapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return Built{}, err
	}
	var failed []string
	for _, a := range s.Accounts {
		if !a.OK {
			failed = append(failed, a.Email)
		}
	}
	if len(s.Accounts) > 0 && len(failed) == len(s.Accounts) {
		return Built{}, fmt.Errorf("no mail account could be read (%s)", strings.Join(failed, ", "))
	}
	b := Built{Refs: map[string]ItemRef{}}
	if len(failed) > 0 {
		b.Warning = "not read: " + strings.Join(failed, ", ")
	}
	g := Group{Items: []Item{}}
	for i, m := range s.Messages {
		if i == mailShown {
			break
		}
		from := m.FromName
		if from == "" {
			from = m.FromAddr
		}
		when := m.Received
		if t, err := time.Parse(time.RFC3339, m.Received); err == nil {
			when = t.In(loc).Format("Mon 2 Jan 15:04")
		}
		subject := m.Subject
		if subject == "" {
			subject = "(no subject)"
		}
		g.Items = append(g.Items, Item{Key: fmt.Sprintf("mail:%d", i), Title: subject,
			Subtitle: from + " · " + when, Detail: m.Snippet})
	}
	b.Groups = []Group{g}
	return b, nil
}

func (MailSource) Act(context.Context, Client, string, ItemRef, json.RawMessage) (ActResult, error) {
	return ActResult{}, ErrNoActions
}
