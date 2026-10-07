# Today

A screen in the TRMNL app (Menu → Today) that shows the widgets a session
gateway places on this tablet's screen, reached through the tablet's
Tailscale proxy. (The gateway runs on a Mac; the messages below call it "the
Mac".) The gateway decides which widgets appear, where, and in which view;
the app knows no widget or app by name. Nothing is sent, and the screen only
says that Today isn't set up, unless `~/.config/trmnl-remarkable/today.json`
exists.

This page describes the code as it is. The tests are beside it, in
`backend/internal/today/`.

## Files on the tablet

| File | What |
|------|------|
| `~/.config/trmnl-remarkable/today.json` (0600) | `gateway_url`, `proxy`, `token_file`, `timezone`, optional `sections` |
| `~/.config/trmnl-remarkable/today.token` (0600) | the gateway bearer; `token_file` names it, and this is the usual place. Never logged or shown |
| `~/.cache/trmnl-remarkable/today.json` (0600) | the screen's last good data |

```json
{
  "gateway_url": "http://gateway.example:8090",
  "proxy": "http://127.0.0.1:1055",
  "token_file": "/home/root/.config/trmnl-remarkable/today.token",
  "timezone": "Europe/London",
  "sections": ["widgets"]
}
```

- `gateway_url` is the gateway's base URL, an `http(s)` URL; the screen is
  fetched from `GET /screens/tablet` under it and actions go to
  `POST /widgets/{id}/actions/{action}`. `proxy` is required: the `http` URL
  of the local proxy that tailscaled listens on, which is how the gateway is
  reached. `timezone` is an IANA name; every time shown uses it (the tablet
  runs UTC).
- `sections` may only name `widgets`, the one source. Left out, it is used.
  Names it doesn't know are logged and ignored; a list with none left makes
  Today say `today.json: no known sections`.
- Keys it doesn't know are an error, so a misspelt key is reported rather
  than silently ignored. The backend reads the config and the token once, at
  start: reopen the app after changing either.
- The token file holds one line. A single trailing newline is stripped, and
  anything that is not a visible ASCII character is refused.
- A `today.json` or token that can't be used shows the screen "Today isn't set
  up on this tablet" with the problem under it (for example
  `today.json: proxy is required`). The problem never contains the token. The
  script that writes these files, `rm-today-setup`, is not part of this
  repository.
- **Settings → Clear cache** always deletes Today's cache, including when
  Today is off or its config is broken, and nothing in flight can bring it
  back. The cache holds the data of every widget on the screen, list rows
  included, so treat it as private.

## What you see

- Under the header, the screen's status: `as of 07:02`, and `offline` (or
  another error) beside a cached screen when the Mac can't be reached. A
  warning about the layout, for example a widget placed outside the grid,
  is added to it.
- **Alerts** run across the top as banners, in reading order. An alert with
  nothing to say takes no space; one that can't be read shows its widget's
  title and why.
- **The grid**: two columns in landscape, at each widget's place; one column
  in portrait, in reading order (top to bottom, then left to right). Sizes
  are small (1×1), wide (2×1), tall (1×2) and large (2×2). Rows nothing uses
  close up. More rows than fit scroll. A screen with nothing placed on it
  says so.
- Each cell has its title and, when needed, a note: `as of 08:30 · offline`
  when the gateway served its cached data because the app is down, or
  `unavailable: <reason>` when it has nothing (the app is down with no
  cache, its data was bad, the widget is gone, or this tablet can't show
  the view). The other cells draw as usual.
- Views: **stat**, a large number with ▲ or ▼ from its delta's sign;
  **list**, rows grouped under optional titles; **spark**, a drawn line
  with its last value and range; **alert**, the banners above.
- Tone shows as a marker and weight: `✓` good, `!` warn (bold), `!!` bad
  (bold); a warn or bad cell has a heavier frame.
- Tapping a list row opens a sheet with its widget, title, subtitle, detail
  and actions. Only low- and medium-risk actions are ever shown. A medium
  action asks first, in the widget's own words, with Yes and Cancel. A
  running action shows `…`. If it is refused the sheet stays open with the
  reason; if it succeeds the sheet closes and the footer shows the message
  for five seconds. Leaving the view closes the sheet.
- The footer has Refresh and Dashboard (back), and says "Refreshing…" while a
  fetch runs. Refresh is disabled while one does.

## How it fits together

- `backend/internal/today`:
  - `config.go`: `Config`, `LoadConfig`, `ReadToken`.
  - `gateway.go`: `Gateway`, the only HTTP client (the proxy, the bearer, a
    15 s timeout, at most 4 MiB read, no redirects), `HTTPError` (status,
    message, outcome), `ErrTailscaleDown` and `UserMessage`.
  - `sources.go`: the `Source` interface, the model sent to the view
    (`Section`, `Screen`, `Cell`, `Group`, `Item`, `Action` and the view
    types), `Registry()` and `Enabled()`.
  - `widgets.go`: `WidgetsSource`, the only source.
  - `engine.go` and `cache.go`: `Engine`, which owns the rules below, and
    the cache file.
- `backend/cmd/trmnl-remarkable/main.go` starts the engine and routes the
  messages. `app/ui/TodayView.qml` draws the screen.

Messages:

| Message | Direction | Payload |
|---------|-----------|---------|
| 18 | QML → backend | `{}`: open or refresh |
| 19 | QML → backend | `{"section", "rev", "action", "key"}`: run an action |
| 109 | backend → QML | `{"configured", "problem", "refreshing", "sections"}` |
| 110 | backend → QML | `{"ok", "section", "action", "key", "message"}`, then a fresh 109 when the action ran |

The section in 109 is `{"id", "rev", "title", "placement", "status",
"as_of", "warning", "error", "groups", "screen"}`. `status` is `ok`
(fetched), `error` (the last fetch failed; cached data may still be there,
with its `as_of`) or `none` (no data yet). `placement` and `groups` are kept
from the Go model; the view reads `screen`, which is
`{"cols", "rows", "banners": [Cell], "cells": [Cell]}`. A cell is
`{"widget", "view", "x", "y", "w", "h", "title", "tone", "note",
"problem"}` plus the one field of its view: `stat` `{value, label, delta,
trend, tone}`, `groups` (list), `spark`
`{label, points (scaled 0..1), min, max, last, unit}` or `alert`
`{text, tone}`. Item keys are `<widget id> <the app's key>`: stable, and
opaque to the view. Times are formatted by the backend, in `timezone`.

## What the tablet says when something is wrong

| Situation | What is shown |
|-----------|---------------|
| The Mac is asleep or its gateway is down (a timeout, a network error, or a 502 to 504 with no message), with a cached screen | The cached screen, the status reading `as of HH:MM · offline` (`as of Mon 5 Oct 23:50 · offline` for an earlier day) |
| The same, with nothing cached | "Can't reach the Mac. Today needs the Mac awake." |
| One app is down; the gateway has its data cached | That cell, `as of HH:MM · offline` |
| One app is down with no cache, its data was bad, the widget was removed, or the view is unknown | That cell, `unavailable: <reason>`; the rest draw |
| The gateway fails another way (an HTTP error, or an answer the tablet can't read) | `unavailable: <reason>` in the status, beside the cached screen if there is one |
| 403 `peer not allowed` | "The Mac doesn't recognise this tablet yet. Try again in a minute." |
| 401, or another 403 from the gateway itself | "Tablet not authorised. Run rm-today-setup." |
| tailscaled isn't running | "Tailscale isn't running on the tablet", with the cache shown |

Actions:

| Answer | What is shown | Then |
|--------|---------------|------|
| 200 `done` | the app's message, or "Done", in the footer | one refetch if the app says the screen changed |
| 409 `refused` | the app's reason in the sheet; the item stays | one refetch |
| 403 `denied` from the widget hub (not on this screen, not permitted here) | the gateway's reason in the sheet | nothing |
| 404 `denied` | the reason ("unknown action") in the sheet | one refetch |
| 502, 503, 504, a timeout, or a dropped connection | "Unknown — check <widget title> in its app" (the gateway's own words, when it sent some) | nothing: never retried |
| 401, or a 403 from the gateway's gate (`peer not allowed`, `service peer not permitted`), though it also says `denied` | the wording in the table above | nothing |
| Any other status | the gateway's message, or `The Mac answered HTTP <status>` | nothing |
| tailscaled isn't running | "Tailscale isn't running on the tablet"; nothing was sent | nothing |
| The engine refuses | "this list changed; check it again" (stale `rev`), "this item is no longer there", "another action is in progress", "unknown section", "this item doesn't offer that action" | — |

## Rules the engine keeps

- One fetch at a time; a Refresh while one runs only re-sends the state.
- One action at a time.
- An action names the section `rev` its sheet was drawn from; a stale one is
  refused without a request. The rev changes whenever what is shown
  changes, so a sheet never acts on content the user didn't see.
- A fetch that started before an action finished is dropped.
- Only a fetch whose data builds replaces the cache, atomically and
  owner-only. A failed fetch keeps the last good screen beside its error.
- Every refresh first rebuilds the cached screen as of now, so `as of`
  notes name the day after midnight.
- Refetch requests are a set, run once after any running fetch.
- Nothing begun before a Clear cache is committed after it.

## Changing what Today shows

Widgets, their views, their places and their actions are decided by the
gateway and the apps behind it; this app needs no change for a new widget
or a new app. Arrange the tablet's screen in the gateway's layout editor,
then tap Refresh.

A change here is needed only for a new **view** type: add its field to
`Cell` in `sources.go`, add the view to `knownViews` and map it in
`buildCell` (`widgets.go`) with a test in `widgets_test.go`, add its
renderer to `TodayView.qml`, and extend the harness payload. Until then a
placement in that view reads `unavailable: this tablet can't show the
"<view>" view`.
