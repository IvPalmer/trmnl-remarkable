package today

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// The fixture's brief was created on 2026-10-05; tests read it the next day.
var nextDay = time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

func TestBriefNullIsAnEmptyBrief(t *testing.T) {
	c := &fakeClient{get: map[string]string{"/brief": `{"brief":null,"run":null}`}}
	raw, err := BriefSource{}.Fetch(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	b, err := BriefSource{}.Build(raw, nextDay, time.UTC)
	if err != nil || len(b.Groups) != 0 || b.Warning != "" || b.Title != "" {
		t.Fatalf("Build = %+v, %v", b, err)
	}
}

func TestBriefSectionsBecomeGroups(t *testing.T) {
	data, err := os.ReadFile("testdata/brief.json")
	if err != nil {
		t.Fatal(err)
	}
	c := &fakeClient{get: map[string]string{"/brief": string(data)}}
	raw, err := BriefSource{}.Fetch(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "outcome") {
		t.Fatalf("raw keeps fields Today does not use: %s", raw)
	}
	b, err := BriefSource{}.Build(raw, nextDay, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Groups) != 2 || b.Groups[0].Title != "Agenda" ||
		b.Groups[0].Items[0].Title != "10h dentista" || b.Groups[0].Items[1].Title != "Ligar banco" {
		t.Fatalf("groups = %+v", b.Groups)
	}
	if b.Warning != "not today's brief (2026-10-05)" {
		t.Fatalf("warning = %q", b.Warning)
	}
	if _, err := (BriefSource{}).Act(context.Background(), c, "x", nil, raw); !errors.Is(err, ErrNoActions) {
		t.Fatalf("Act err = %v", err)
	}
}

func TestBriefKeepsItsOwnTitle(t *testing.T) {
	for _, c := range []struct {
		name, json, want string
	}{
		{"set", `{"brief":{"title":"Bom dia, segunda","is_today":true,"sections":[]}}`, "Bom dia, segunda"},
		{"trimmed", `{"brief":{"title":"  Bom dia  ","is_today":true,"sections":[]}}`, "Bom dia"},
		{"empty", `{"brief":{"title":"","is_today":true,"sections":[]}}`, ""},
		{"blank", `{"brief":{"title":"   ","is_today":true,"sections":[]}}`, ""},
		{"absent", `{"brief":{"is_today":true,"sections":[]}}`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			fc := &fakeClient{get: map[string]string{"/brief": c.json}}
			raw, err := BriefSource{}.Fetch(context.Background(), fc)
			if err != nil {
				t.Fatal(err)
			}
			b, err := BriefSource{}.Build(raw, nextDay, time.UTC)
			if err != nil || b.Title != c.want {
				t.Fatalf("Title = %q, %v; want %q", b.Title, err, c.want)
			}
			if b.Warning != "" {
				t.Fatalf("a brief for today warns %q", b.Warning)
			}
		})
	}
	if got := (BriefSource{}).Title(); got != "Brief" {
		t.Fatalf("static Title() = %q, want the fallback Brief", got)
	}
}

// is_today is frozen when the gateway answers; the date is what ages.
func TestBriefIsTodayFollowsTheClock(t *testing.T) {
	brt := time.FixedZone("BRT", -3*60*60)
	const raw = `{"brief":{"created":"2026-10-05","is_today":true,"sections":[]}}`
	for _, c := range []struct {
		name string
		now  time.Time
		loc  *time.Location
		want string
	}{
		{"its own day", time.Date(2026, 10, 5, 23, 30, 0, 0, brt), brt, ""},
		{"after midnight", time.Date(2026, 10, 6, 0, 30, 0, 0, brt), brt, "not today's brief (2026-10-05)"},
		// 01:30 UTC on the 6th is still 22:30 on the 5th in loc.
		{"zone decides the day", time.Date(2026, 10, 6, 1, 30, 0, 0, time.UTC), brt, ""},
		{"same instant, UTC day", time.Date(2026, 10, 6, 1, 30, 0, 0, time.UTC), time.UTC, "not today's brief (2026-10-05)"},
		{"nil zone reads as UTC", time.Date(2026, 10, 6, 1, 30, 0, 0, time.UTC), nil, "not today's brief (2026-10-05)"},
		{"a brief from tomorrow is not today's", time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC), time.UTC, "not today's brief (2026-10-05)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, err := BriefSource{}.Build([]byte(raw), c.now, c.loc)
			if err != nil || b.Warning != c.want {
				t.Fatalf("Warning = %q, %v; want %q", b.Warning, err, c.want)
			}
		})
	}
}

// Without a usable created date the gateway's own is_today is all there is.
func TestBriefFallsBackToIsTodayWithoutADate(t *testing.T) {
	for _, c := range []struct {
		name, brief, want string
	}{
		{"empty, today", `{"created":"","is_today":true}`, ""},
		{"empty, not today", `{"created":"","is_today":false}`, "not today's brief"},
		{"absent, not today", `{"is_today":false}`, "not today's brief"},
		{"unparseable, today", `{"created":"yesterday","is_today":true}`, ""},
		{"unparseable, not today", `{"created":"yesterday","is_today":false}`, "not today's brief"},
	} {
		t.Run(c.name, func(t *testing.T) {
			raw := `{"brief":` + c.brief[:len(c.brief)-1] + `,"sections":[]}}`
			b, err := BriefSource{}.Build([]byte(raw), nextDay, time.UTC)
			if err != nil || b.Warning != c.want {
				t.Fatalf("Warning = %q, %v; want %q", b.Warning, err, c.want)
			}
		})
	}
}

func TestBriefFetchFailsWhenTheGatewayDoes(t *testing.T) {
	want := &HTTPError{Status: 503}
	c := &fakeClient{err: map[string]error{"/brief": want}}
	if _, err := (BriefSource{}).Fetch(context.Background(), c); !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestEnabledPicksInOrderAndReportsUnknown(t *testing.T) {
	all, unknown := Enabled(nil)
	reg := Registry()
	if len(unknown) != 0 || len(all) != len(reg) {
		t.Fatalf("Enabled(nil) = %d sources, unknown %v", len(all), unknown)
	}
	got, unknown := Enabled([]string{"brief", "nope", "brief"})
	if len(got) != 1 || got[0].ID() != "brief" || len(unknown) != 1 || unknown[0] != "nope" {
		t.Fatalf("Enabled = %v, unknown %v", got, unknown)
	}
}

func TestRegistryIsWellFormed(t *testing.T) {
	ids := map[string]bool{}
	for _, s := range Registry() {
		id := s.ID()
		if id == "" || ids[id] {
			t.Errorf("source ID %q is empty or repeated", id)
		}
		ids[id] = true
		if s.Title() == "" {
			t.Errorf("source %q has no Title", id)
		}
		if p := s.Placement(); p != Banner && p != Column {
			t.Errorf("source %q has Placement %q, want Banner or Column", id, p)
		}
	}
	if len(ids) == 0 {
		t.Fatal("Registry is empty")
	}
}

func TestPlainDropsMarkdownMarks(t *testing.T) {
	if got := plain("**10h** `x` y"); got != "10h x y" {
		t.Fatalf("plain = %q", got)
	}
}
