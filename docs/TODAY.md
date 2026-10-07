# Today

A screen in the TRMNL app (Menu → Today) that shows sections of your day from
a session gateway, reached through the tablet's Tailscale proxy. (The gateway
runs on a Mac; the messages below call it "the Mac".) The built-in sections
are the day's brief, dated personal items you can tick off, and important
mail. It does nothing, and sends no request, unless
`~/.config/trmnl-remarkable/today.json` exists. Without it the screen only
says that Today isn't set up.

This page describes the code as it is. The tests are beside it, in
`backend/internal/today/`.

## Files on the tablet

| File | What |
|------|------|
| `~/.config/trmnl-remarkable/today.json` (0600) | `gateway_url`, `proxy`, `token_file`, `timezone`, optional `sections` |
| `~/.config/trmnl-remarkable/today.token` (0600) | the gateway bearer; `token_file` names it, and this is the usual place. Never logged or shown |
| `~/.cache/trmnl-remarkable/today.json` (0600) | each section's last good data |

```json
{
  "gateway_url": "http://gateway.example:8090",
  "proxy": "http://127.0.0.1:1055",
  "token_file": "/home/root/.config/trmnl-remarkable/today.token",
  "timezone": "Europe/London",
  "sections": ["brief", "due", "mail"]
}
```

- `gateway_url` is an `http(s)` URL. `proxy` is required: the `http` URL of
  the local proxy that tailscaled listens on, which is how the gateway is
  reached. `timezone` is an IANA name; "today" and every time shown use it
  (the tablet itself runs UTC).
- `sections` chooses which sections are shown, and in what order. Left out,
  every section in `Registry()` is shown in its default order (`brief`, `due`,
  `mail`). Names it doesn't know are logged and ignored, and a list with none
  left makes Today say `today.json: no known sections`.
- Keys it doesn't know are an error, so a misspelt key is reported rather than
  silently ignored.
- The backend reads the config and token once, at start: reopen the app after
  changing either.
- The token file holds one line. A single trailing newline is stripped, and
  anything that is not a visible ASCII character is refused.
- A `today.json` or token that can't be used shows the screen "Today isn't set
  up on this tablet" with the problem under it (for example
  `today.json: proxy is required`). The problem never contains the token. The
  script that writes these files, `rm-today-setup`, is not part of this
  repository.
- **Settings → Clear cache** always deletes Today's cache, including when Today
  is off or its config is broken, and anything already in flight can no longer
  bring it back. The cache holds personal items, mail subjects and snippets.

## What you see

- A banner section runs across the top: its title and status, then up to four
  groups, one or two lines each (`Group title: item · item`). Banner lines
  can't be tapped.
- Column sections sit below, in the order the backend sends them: two across
  in landscape, one in portrait. Each has its title, a status line, and rows
  grouped under optional group titles. The status line reads
  `as of 07:02 · <error> · <warning>`; a section with no data yet says its
  error or "No data yet", and one with data but no items says "Nothing here".
- Tapping a row opens a sheet with the item's full title, subtitle and detail,
  and one button per action. A running action shows `…` on its button. If the
  action is refused the sheet stays open, shows the reason, and the item stays;
  if it succeeds the sheet closes and the footer shows the message for five
  seconds. Leaving the view closes the sheet.
- The footer has Refresh and Dashboard (back), and says "Refreshing…" while a
  fetch runs. Refresh is disabled while one does.
- When every section has no data and the same error (the Mac is unreachable
  and nothing is cached), the error is said once, in the middle, not in every
  column.

Opening Today asks the backend for its state and a refresh. The view draws the
cached data at once, then again when the fetch ends. Optional fields
(`subtitle`, `detail`, `actions`, `as_of`, `warning`, `error`) are left out of
the JSON when empty, and the QML never binds an undefined one.

## How it fits together

- `backend/internal/today`:
  - `config.go`: `Config`, `LoadConfig` and `ReadToken`.
  - `gateway.go`: `Gateway`, the only HTTP client: it goes through the proxy
    with the bearer, times out at 15 seconds, reads at most 4 MiB of an
    answer, and never follows a redirect, so the bearer can't be sent anywhere
    else. Sources see it only as the `Client` interface (`Get`, `Post`). It
    also holds `HTTPError`, `ErrTailscaleDown` and `UserMessage`, the text the
    operator reads for an error.
  - `sources.go`: the `Source` interface, the item and section types,
    `Registry()` and `Enabled()`.
  - `brief.go`, `due.go`, `mail.go`: the built-in `Source`s, one file each.
  - `engine.go` and `cache.go`: `Engine`, which owns the rules below, and the
    cache file.
- `backend/cmd/trmnl-remarkable/main.go` starts the engine (`openToday`) and
  routes the messages.
- `app/ui/TodayView.qml` draws any section; it names no source. Menu → Today
  and the message wiring are in `app/ui/TRMNL.qml`.

Messages:

| Message | Direction | Payload |
|---------|-----------|---------|
| 18 | QML → backend | `{}`: open or refresh |
| 19 | QML → backend | `{"section", "rev", "action", "key"}`: run an action |
| 109 | backend → QML | `{"configured", "problem", "refreshing", "sections"}`: a snapshot of every section |
| 110 | backend → QML | `{"ok", "section", "action", "key", "message"}`: an action's result, followed by a fresh 109 |

A section in message 109 is `{"id", "rev", "title", "placement", "status",
"as_of", "warning", "error", "groups"}`. `placement` is `banner` or `column`.
`status` is `ok` (fetched), `error` (the last fetch failed; cached data may
still be there, with its `as_of`) or `none` (no data yet). `groups` is always
an array, never null. Times are formatted by the backend, in `timezone`.

### The built-in sections

| Section | Route | Shows | Actions |
|---------|-------|-------|---------|
| `brief` (banner) | `GET /brief` | the latest brief, titled with its own heading. Warns "not today's brief (2026-10-05)" when its date isn't today in `timezone`, decided when it is shown, so a brief cached yesterday warns after midnight | none |
| `due` (column) | `GET /personal` | open items with a date, from overdue through the next 7 days, grouped Overdue, Today and Next 7 days | Done, which ticks the item (`POST /personal/items`). An item over 300 characters has no Done: the gateway refuses to tick it, and counts characters, not bytes |
| `mail` (column) | `GET /mail` | the first 10 important messages. Warns `not read: <account>` for an account that failed; an error only if every account failed | none |

Each `Fetch` keeps only the fields it shows, so message ids, thread ids and
URLs never reach the tablet's cache.

## What the tablet says when something is wrong

| Situation | What is shown |
|-----------|---------------|
| The Mac is asleep or the gateway is down (timeout, network error, or 502 to 504 with no message), and a section has cached data | The cached data, marked `as of HH:MM · offline` |
| The same, with nothing cached | "Can't reach the Mac. Today needs the Mac awake." |
| One section's fetch fails otherwise (an HTTP error, or data it can't use) | That section says `unavailable: <reason>`, beside its cached data if it has any. The others show as usual |
| 403 with `peer not allowed` | "The Mac doesn't recognise this tablet yet. Try again in a minute." |
| 401, or any other 403 | "Tablet not authorised. Run rm-today-setup." |
| tailscaled isn't running (the proxy refuses the connection) | "Tailscale isn't running on the tablet", with the cache shown |
| Tick answered 409 | The item stays; the sheet shows the gateway's reason; `due` is fetched once more |
| A tick fails any other way (403, 503, a timeout) | The item stays; the sheet shows the error, in the wording above. No refetch |
| The engine refuses an action | "this list changed; check it again" (stale `rev`), "this item is no longer there", "another action is in progress" or "unknown section" |

## Rules every section gets for free

These belong to `Engine`; a `Source` doesn't have to do anything for them.

- One fetch round at a time. A refresh while one is running only re-sends the
  state. The sections of a round are fetched concurrently.
- One action at a time.
- An action names the section `rev` it was drawn from. A stale one is refused
  without any request to the gateway.
- A section's `rev` changes whenever what it shows changes: a fetch or a
  rebuild that differs in any way (group titles, keys, details), an action that
  returns new data, and Clear cache. A fetch that finds nothing changed keeps
  it, so the sheet on screen can still act after Today is reopened. Revs start
  from the clock, so a sheet drawn before a restart never matches one drawn
  after it.
- A fetch that started before an action on the same section finished is
  dropped: the action's data is newer.
- A fetch counts as a success only if `Build` accepts its data. Only then is
  the section's data replaced, in memory and in the cache. A failed fetch or
  build keeps the last good data and shows the error beside it.
- The cache file is replaced atomically (a temporary file in the same folder,
  owner-only, then renamed). It holds each section's raw data and when it was
  fetched, not what was drawn: the sections are rebuilt from it when it is
  read. A missing, corrupt or unknown-version cache is an empty one.
- Every refresh first rebuilds cached data as of now, so a view left open
  across midnight regroups ("Today" becomes "Overdue") even if the fetch fails.
- Refetch requests are a set. A refetch an action asks for waits for any
  running fetch to end; then every section in the set is fetched once,
  together. An action that fails and asks for a refetch in the same result
  still gets one.
- Nothing begun before a Clear cache is committed after it.

## Adding a section

A section is one Go file that implements `Source`, plus one line in
`Registry()`. The QML needs no change. As an example, a reading list that the
gateway serves at `GET /reading`:

```go
package today

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ReadingSource lists the gateway's reading list. Marking an item read is its
// one action.
type ReadingSource struct{}

func (ReadingSource) ID() string           { return "reading" }
func (ReadingSource) Title() string        { return "Reading" }
func (ReadingSource) Placement() Placement { return Column }

type readingList struct {
	Items []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Site  string `json:"site"`
	} `json:"items"`
}

// Fetch keeps only what is used. What it returns is what gets cached.
func (ReadingSource) Fetch(ctx context.Context, c Client) (json.RawMessage, error) {
	var l readingList
	if err := c.Get(ctx, "/reading", &l); err != nil {
		return nil, err
	}
	return json.Marshal(l)
}

// Build must be a pure function of raw, now and loc: it runs again on cached
// data, after midnight, and on whatever Act returns. Keys only have to be
// unique within the section. Refs are the private handles Act gets back; they
// never leave the backend.
func (ReadingSource) Build(raw json.RawMessage, now time.Time, loc *time.Location) (Built, error) {
	var l readingList
	if err := json.Unmarshal(raw, &l); err != nil {
		return Built{}, err
	}
	b := Built{Refs: map[string]ItemRef{}}
	g := Group{Items: []Item{}}
	for i, it := range l.Items {
		key := fmt.Sprintf("reading:%d", i)
		b.Refs[key] = ItemRef{"id": it.ID}
		g.Items = append(g.Items, Item{
			Key: key, Title: it.Title, Subtitle: it.Site,
			Actions: []Action{{ID: "read", Label: "Mark read"}},
		})
	}
	b.Groups = []Group{g}
	return b, nil
}

// Act is called with the action's ID and the item's Ref. A read-only section
// returns ErrNoActions here instead.
func (ReadingSource) Act(ctx context.Context, c Client, action string, ref ItemRef, raw json.RawMessage) (ActResult, error) {
	if action != "read" {
		return ActResult{}, fmt.Errorf("unknown action %q", action)
	}
	var l readingList
	if err := c.Post(ctx, "/reading/read", map[string]string{"id": ref["id"]}, &l); err != nil {
		return ActResult{}, err
	}
	nb, err := json.Marshal(l)
	if err != nil {
		return ActResult{Refetch: true}, nil
	}
	return ActResult{Raw: nb, Message: "Marked read"}, nil
}
```

Then:

1. Save it as `backend/internal/today/reading.go`. What `Fetch` returns, what
   `Build` reads and what `Act` returns in `Raw` all have the same shape.
2. Add it to `Registry()` in `sources.go`:

   ```go
   return []Source{BriefSource{}, DueSource{}, MailSource{}, ReadingSource{}}
   ```

   A tablet with no `sections` in `today.json` shows it at once. One with
   `sections` has to add `"reading"` to the list.
3. Update the default order asserted in `sources_test.go`
   (`TestEnabledFollowsTheConfiguredOrder`).
4. Write `reading_test.go` against the fake client in `helpers_test.go`, with
   an invented fixture (inline JSON, or a file in `testdata/` when it is long):

   ```go
   package today

   import (
   	"context"
   	"testing"
   	"time"
   )

   func TestReadingMarksAnItemRead(t *testing.T) {
   	c := &fakeClient{
   		get: map[string]string{"/reading": `{"items":[{"id":"r1","title":"A long read","site":"example.com"}]}`},
   		post: func(path string, body any) (string, error) {
   			if path != "/reading/read" {
   				return "", &HTTPError{Status: 404, Message: "no route"}
   			}
   			return `{"items":[]}`, nil
   		},
   	}
   	src, ctx, now := ReadingSource{}, context.Background(), time.Now()
   	raw, err := src.Fetch(ctx, c)
   	if err != nil {
   		t.Fatal(err)
   	}
   	built, err := src.Build(raw, now, time.UTC)
   	if err != nil || len(built.Groups[0].Items) != 1 {
   		t.Fatalf("Build = %+v, %v", built, err)
   	}
   	item := built.Groups[0].Items[0]
   	res, err := src.Act(ctx, c, "read", built.Refs[item.Key], raw)
   	if err != nil {
   		t.Fatal(err)
   	}
   	after, err := src.Build(res.Raw, now, time.UTC)
   	if err != nil || len(after.Groups[0].Items) != 0 {
   		t.Fatalf("after Act: %+v, %v", after, err)
   	}
   }
   ```

   Test the failure paths too: a gateway error from `Fetch`, data `Build`
   rejects, and (with an action) a refused `Act`.
5. If the route is new to the tablet, the gateway has to allow it for the
   tablet's grant before the section can work. That change is made on the
   gateway, not here. Until then the section's fetch fails and says so, as the
   table above describes.
6. Run `go test ./...` and `go vet ./...` from the repository root. Build and
   install as the README's "This fork" section describes (copy the built
   backend and `resources.rcc` over the existing install), restart xochitl,
   and reopen TRMNL.

Notes for `Source` authors:

- `ID()` is the name used in `sections`, as the cache key and in message 19.
  Keep it short and lower-case, and don't rename it once it ships: a renamed
  section starts with an empty cache.
- `Fetch` and `Act` get a context and the `Client`; the gateway does the
  timeout. They run without the engine's lock held, so a slow gateway never
  freezes the view.
- `Build` returning an error means the data is unusable: the fetch counts as
  failed and nothing is cached. Use it for "the whole answer is an error" (as
  `mail` does when every account failed), not for one bad item. Skip that item.
- `Built.Title`, when set, replaces `Title()` as the section's heading. `brief`
  uses it to carry the brief's own title; most sources leave it empty.
- `Built.Warning` is a note shown beside the status while the data is still
  good.
- Give `Group.Items` a non-nil slice, and leave a group's title empty for a
  section that doesn't need group headings.
- A banner section is read-only in practice: its lines can't be tapped.

## Adding an action

An action is the item's `Actions` in `Build` and a case for its ID in `Act`
(the `read` action above). No QML change: the sheet draws whatever actions the
item carries.

1. Add `Action{ID, Label}` to the items that offer it in `Build`, and a `case`
   for the ID in `Act`. Offer an action only when the gateway will accept it;
   `due` leaves Done off an item the gateway would refuse.
2. `Act` answers with an `ActResult`, and the engine does the rest:
   - `Raw`: the section's new data, in the same shape `Fetch` returns, when
     the gateway answers with it (as `read` and `due`'s tick do). The engine
     rebuilds the section, bumps its `rev` and replaces the cache.
   - `Refetch: true`: the list may be stale, so fetch it once more. Use it
     when the answer doesn't carry the new data.
   - `Message`: the text shown in the footer on success. Empty means "Done".
   - An error is shown in the sheet, in the wording of `UserMessage`, and the
     item stays. If the error means the list is stale (as `due`'s 409 does),
     return it together with `Refetch: true`. Other failures should not
     refetch.
3. The engine finds an item's `Ref` by its `Key`. A `Key` that is a position
   in a list (`due`'s are) points to a different item after a tick or a
   refetch; the `rev` check is the only thing that keeps a stale sheet from
   acting on it. Don't loosen that check, and use a stable id in the key when
   the gateway gives you one.
4. If it writes through a gateway operation the tablet's grant doesn't allow
   yet, the gateway has to allow it first, as in step 5 above.
5. Test the success path (the new `Raw` builds without the item), the refusal
   paths (a 409 refetches; a 403 and a 503 don't) and that an item the
   gateway would refuse has no action, with `fakeClient`.
