package today

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 22:30 on Tuesday 6 October in São Paulo, already the 7th in UTC.
var spNow = time.Date(2026, 10, 7, 1, 30, 0, 0, time.UTC)

func saoPaulo(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func dueFixture(t *testing.T) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile("testdata/personal.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := DueSource{}.Fetch(context.Background(), &fakeClient{get: map[string]string{"/personal": string(data)}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func groupTitles(g Group) []string {
	var out []string
	for _, it := range g.Items {
		out = append(out, it.Title)
	}
	return out
}

func TestDueGroupsByTheOperatorsDayNotUTC(t *testing.T) {
	b, err := DueSource{}.Build(dueFixture(t), spNow, saoPaulo(t))
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	got := map[string][]string{}
	for _, g := range b.Groups {
		order = append(order, g.Title)
		got[g.Title] = groupTitles(g)
	}
	if strings.Join(order, "|") != "Overdue|Today|Next 7 days" {
		t.Fatalf("groups = %v", order)
	}
	want := map[string][]string{
		"Overdue":     {"Pagar IPTU até 2026-10-05"},
		"Today":       {"Ligar para o encanador 06/10/2026", "Revisão 2026-10-06"},
		"Next 7 days": {"Renovar seguro 2026-10-13"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}

func TestDueItemsKeepTheExactTextOnlyInTheirRefs(t *testing.T) {
	b, _ := DueSource{}.Build(dueFixture(t), spNow, saoPaulo(t))
	it := b.Groups[0].Items[0]
	if it.Key != "due:casa.md#0" || it.Subtitle != "Mon 5 Oct · Casa" || it.Detail != "Pagar **IPTU** até 2026-10-05" {
		t.Fatalf("item = %+v", it)
	}
	if ref := b.Refs[it.Key]; ref["file"] != "casa.md" || ref["text"] != "Pagar **IPTU** até 2026-10-05" {
		t.Fatalf("ref = %v", ref)
	}
	if len(it.Actions) != 1 || it.Actions[0].ID != "tick" {
		t.Fatalf("actions = %+v", it.Actions)
	}
	if carro := b.Groups[1].Items[1]; carro.Key != "due:carro.md#0" || carro.Subtitle != "Tue 6 Oct · Carro" {
		t.Fatalf("carro item = %+v", carro)
	}
	if b.Title != "" {
		t.Fatalf("Due brings no title of its own, got %q", b.Title)
	}
}

func TestAFileWithoutATitleIsLabelledByItsStem(t *testing.T) {
	raw := json.RawMessage(`{"exists":true,"files":[{"name":"carro.md","title":"","open":[{"text":"Revisão","due":"2026-10-06","section":"x"}]}]}`)
	b, err := DueSource{}.Build(raw, spNow, saoPaulo(t))
	if err != nil || len(b.Groups) != 1 || b.Groups[0].Items[0].Subtitle != "Tue 6 Oct · carro" {
		t.Fatalf("Build = %+v, %v", b, err)
	}
}

// The gateway refuses a tick over 300 characters, counting characters and not
// bytes, so such an item is shown without a Done button.
func TestAnItemTheGatewayWouldRefuseGetsNoDoneAction(t *testing.T) {
	long := strings.Repeat("é", 301) // 301 runes, 602 bytes
	edge := strings.Repeat("é", 300) // 300 runes, 600 bytes: still fine
	raw, err := json.Marshal(personalSnapshot{Files: []personalFile{{
		Name: "casa.md", Title: "Casa",
		Open: []personalItem{
			{Text: long, Due: "2026-10-06"},
			{Text: edge, Due: "2026-10-06"},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := DueSource{}.Build(raw, spNow, saoPaulo(t))
	if err != nil || len(b.Groups) != 1 || len(b.Groups[0].Items) != 2 {
		t.Fatalf("Build = %+v, %v", b, err)
	}
	over, edgeItem := b.Groups[0].Items[0], b.Groups[0].Items[1]
	if over.Title != long || len(over.Actions) != 0 {
		t.Fatalf("301 characters: title kept = %v, actions = %+v", over.Title == long, over.Actions)
	}
	if _, ok := b.Refs[over.Key]; !ok {
		t.Fatal("the long item lost its ref")
	}
	if edgeItem.Title != edge || len(edgeItem.Actions) != 1 || edgeItem.Actions[0].ID != "tick" {
		t.Fatalf("300 characters: actions = %+v", edgeItem.Actions)
	}
}

func TestAnUnreadablePersonalFolderFailsTheSection(t *testing.T) {
	if _, err := (DueSource{}).Build(json.RawMessage(`{"exists":false,"files":[]}`), spNow, saoPaulo(t)); err == nil {
		t.Fatal("want an error")
	}
}

func TestTickSendsTheGatewaysOwnTextAndReturnsTheNewFile(t *testing.T) {
	c := &fakeClient{post: func(string, any) (string, error) {
		return `{"ok":true,"file":{"name":"casa.md","title":"Casa","open":[{"text":"Renovar seguro 2026-10-13","due":"2026-10-13","section":"Pendências"}]}}`, nil
	}}
	res, err := DueSource{}.Act(context.Background(), c, "tick",
		ItemRef{"file": "casa.md", "text": "Pagar **IPTU** até 2026-10-05"}, dueFixture(t))
	if err != nil || res.Refetch || res.Raw == nil {
		t.Fatalf("Act = %+v, %v", res, err)
	}
	var sent map[string]string
	if err := json.Unmarshal([]byte(c.posts[0].body), &sent); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"op": "tick", "file": "casa.md", "text": "Pagar **IPTU** até 2026-10-05"}
	if c.posts[0].path != "/personal/items" || !reflect.DeepEqual(sent, want) {
		t.Fatalf("sent %s %s", c.posts[0].path, c.posts[0].body)
	}
	b, _ := DueSource{}.Build(res.Raw, spNow, saoPaulo(t))
	var all []string
	for _, g := range b.Groups {
		all = append(all, groupTitles(g)...)
	}
	if strings.Contains(strings.Join(all, "|"), "IPTU") || !strings.Contains(strings.Join(all, "|"), "Revisão 2026-10-06") {
		t.Fatalf("after tick: %v", all)
	}
}

func TestTickConflictAsksForARefetch(t *testing.T) {
	for _, msg := range []string{"no open item matches", "more than one open item matches", "file changed, retry"} {
		c := &fakeClient{post: func(string, any) (string, error) { return "", &HTTPError{Status: 409, Message: msg} }}
		res, err := DueSource{}.Act(context.Background(), c, "tick", ItemRef{"file": "casa.md", "text": "x"}, dueFixture(t))
		if err == nil || !res.Refetch || res.Raw != nil || UserMessage(err) != msg {
			t.Fatalf("%s: Act = %+v, %v", msg, res, err)
		}
	}
}

// Only a 409 means the list is stale; every other refusal leaves it alone.
func TestTickRefusalsDoNotRefetch(t *testing.T) {
	for _, status := range []int{400, 401, 403, 500, 503} {
		c := &fakeClient{post: func(string, any) (string, error) {
			return "", &HTTPError{Status: status, Message: "refused"}
		}}
		res, err := DueSource{}.Act(context.Background(), c, "tick", ItemRef{"file": "casa.md", "text": "x"}, dueFixture(t))
		if err == nil || res.Refetch || res.Raw != nil {
			t.Fatalf("%d: Act = %+v, %v", status, res, err)
		}
	}
}

func TestTickForAFileNotInTheSnapshotRefetches(t *testing.T) {
	c := &fakeClient{post: func(string, any) (string, error) {
		return `{"ok":true,"file":{"name":"renamed.md","title":"","open":[]}}`, nil
	}}
	res, err := DueSource{}.Act(context.Background(), c, "tick", ItemRef{"file": "renamed.md", "text": "x"}, dueFixture(t))
	if err != nil || !res.Refetch || res.Raw != nil {
		t.Fatalf("Act = %+v, %v", res, err)
	}
}

func TestDueRejectsUnknownActionsWithoutARequest(t *testing.T) {
	c := &fakeClient{post: func(string, any) (string, error) { return `{}`, nil }}
	if _, err := (DueSource{}).Act(context.Background(), c, "delete", ItemRef{"file": "casa.md", "text": "x"}, dueFixture(t)); err == nil || len(c.posts) != 0 {
		t.Fatalf("err = %v, posts = %d", err, len(c.posts))
	}
}
