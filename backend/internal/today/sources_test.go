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
	got, unknown := Enabled([]string{"mail", "nope", "due", "mail"})
	if ids(got) != "mail,due" || strings.Join(unknown, ",") != "nope" {
		t.Fatalf("Enabled = %s, unknown %v", ids(got), unknown)
	}
	if all, _ := Enabled(nil); ids(all) != "brief,due,mail" {
		t.Fatalf("default = %s", ids(all))
	}
}
