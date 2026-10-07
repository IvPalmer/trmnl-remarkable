package today

import (
	"strings"
	"testing"
)

func ids(ss []Source) string {
	var out []string
	for _, s := range ss {
		out = append(out, s.ID())
	}
	return strings.Join(out, ",")
}

func TestEnabledFollowsTheConfiguredOrder(t *testing.T) {
	got, unknown := Enabled([]string{"brief", "widgets", "widgets"})
	if ids(got) != "widgets" || strings.Join(unknown, ",") != "brief" {
		t.Fatalf("Enabled = %s, unknown %v", ids(got), unknown)
	}
	if all, _ := Enabled(nil); ids(all) != "widgets" {
		t.Fatalf("default = %s", ids(all))
	}
}

// Every registered source has a unique ID, a title and a known placement.
func TestRegistryIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Registry() {
		id := s.ID()
		if id == "" || seen[id] {
			t.Errorf("source ID %q is empty or repeated", id)
		}
		seen[id] = true
		if s.Title() == "" {
			t.Errorf("source %q has no Title", id)
		}
		if p := s.Placement(); p != Banner && p != Column {
			t.Errorf("source %q has Placement %q, want Banner or Column", id, p)
		}
	}
	if len(seen) == 0 {
		t.Fatal("Registry is empty")
	}
}
