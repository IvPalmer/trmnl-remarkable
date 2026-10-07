package today

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func mailJSON(n int, accounts string) string {
	var msgs []string
	for i := 0; i < n; i++ {
		msgs = append(msgs, fmt.Sprintf(`{"id":"m%d","thread_id":"t%d","account":"a@example.com","from_name":"Ana %d","from_addr":"ana@example.com","subject":"Assunto %d","snippet":"Trecho %d","received":"2026-10-06T18:04:00+00:00","url":"https://mail.example/%d"}`, i, i, i, i, i, i))
	}
	return fmt.Sprintf(`{"fetched_at":"2026-10-06T20:00:00+00:00","accounts":%s,"messages":[%s]}`, accounts, strings.Join(msgs, ","))
}

func TestMailKeepsTenAndNoIdsOrURLs(t *testing.T) {
	c := &fakeClient{get: map[string]string{"/mail": mailJSON(12, `[{"email":"a@example.com","ok":true,"count":12,"error":""}]`)}}
	raw, err := MailSource{}.Fetch(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"thread_id", "mail.example", `"id"`} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("raw keeps %s: %s", leak, raw)
		}
	}
	b, err := MailSource{}.Build(raw, spNow, saoPaulo(t))
	if err != nil {
		t.Fatal(err)
	}
	items := b.Groups[0].Items
	if len(items) != 10 || items[0].Title != "Assunto 0" || items[0].Subtitle != "Ana 0 · Tue 6 Oct 15:04" ||
		items[0].Detail != "Trecho 0" || len(items[0].Actions) != 0 {
		t.Fatalf("items[0] = %+v (of %d)", items[0], len(items))
	}
}

func TestMailPartialFailureWarnsAndTotalFailureFails(t *testing.T) {
	two := `[{"email":"a@example.com","ok":true},{"email":"b@example.com","ok":false,"error":"token expired"}]`
	b, err := MailSource{}.Build(json.RawMessage(mailJSON(1, two)), spNow, time.UTC)
	if err != nil || b.Warning != "not read: b@example.com" {
		t.Fatalf("Build = %+v, %v", b, err)
	}
	none := `[{"email":"a@example.com","ok":false},{"email":"b@example.com","ok":false}]`
	if _, err := (MailSource{}).Build(json.RawMessage(mailJSON(0, none)), spNow, time.UTC); err == nil {
		t.Fatal("want an error when no account could be read")
	}
	if _, err := (MailSource{}).Build(json.RawMessage(mailJSON(0, `[]`)), spNow, time.UTC); err != nil {
		t.Fatalf("no accounts configured is not an error: %v", err)
	}
}

func TestMailFetchCapsWhatIsCached(t *testing.T) {
	c := &fakeClient{get: map[string]string{"/mail": mailJSON(30, `[{"email":"a@example.com","ok":true}]`)}}
	raw, err := MailSource{}.Fetch(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), `"subject"`); n != mailShown {
		t.Fatalf("cached %d messages, want %d", n, mailShown)
	}
}
