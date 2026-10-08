# BYOS extensions

Three things that matter to people running their own BYOS server (a server
that speaks the TRMNL Device API). All of them are optional: a server that
knows nothing about them works exactly as before.

- [Tap regions](#tap-regions): a server can mark parts of the image it serves
  as tappable, and the tablet turns a tap into an action.
- [A proxy for tailnet servers](#the-proxy-setting): the app can reach a server
  over a tailnet through a local proxy, with plain HTTP allowed for that hop.
- [The access token on image downloads](#the-access-token-on-image-downloads).

This page describes the code as it is. The tests are beside it, in
`backend/internal/trmnl/`, `backend/internal/config/` and
`backend/cmd/trmnl-remarkable/`.

## Tap regions

### What the server sends

The display answer (the response to the app's `/api/display` call, and to the
current-screen calls it makes when it refreshes without advancing) may carry
two more fields next to `image_url`:

```json
{
  "image_url": "https://byos.example/screens/page1.png",
  "refresh_rate": 900,
  "taps": [
    {
      "x": 40, "y": 120, "w": 520, "h": 90,
      "widget": "mail.inbox",
      "key": "msg-1042",
      "title": "Invoice from Acme",
      "actions": [
        {"id": "archive", "label": "Archive"},
        {"id": "delete", "label": "Delete", "confirm": "Delete {title}?"}
      ]
    }
  ],
  "tap_screen": "page1"
}
```

- `x`, `y`, `w`, `h` are a rectangle in **pixels of the image that was
  served**, from its top-left corner.
- `widget` names the widget the region belongs to, `key` the item in it, and
  `title` is the words the tablet shows for it. `actions` are the buttons.
- An action has an `id`, a `label` (the button's text) and an optional
  `confirm`: a question the tablet asks before running it. `{title}` in it is
  replaced by the region's `title`.
- `tap_screen` names the screen the image was rendered for. The tablet sends
  it back with every action so the server can check that the region is still
  on the screen it was drawn for.

The server decides what a tap may do. The tablet never invents an action: it
only offers what the server listed, and only sends back the ids it was given.

### Validation

The tablet checks the whole list before it uses any of it.

| Field | Rule |
|-------|------|
| `taps` | Absent, `null` or `[]` means no taps and is not an error. Otherwise a list of objects, at most 100 |
| `tap_screen` | Required whenever `taps` is not empty: a string matching `^[a-z][a-z0-9]{0,15}$` |
| `x`, `y` | Integers from 0 to 10000 |
| `w`, `h` | Integers from 1 to 10000 |
| `widget` | `app.id`: `^[a-z][a-z0-9]{0,15}\.[a-z][a-z0-9_]{0,31}$` |
| `key` | Required, at most 500 characters |
| `title` | Optional, at most 500 characters |
| `actions` | At most 8 per region; may be empty |
| action `id` | `^[a-z][a-z0-9_]{0,31}$` |
| action `label` | Required, at most 500 characters |
| action `confirm` | Optional, at most 500 characters |

Coordinates must be JSON numbers with no fraction: `"5"` and `10.5` are
refused.
Lengths count characters, not bytes. Keys the tablet doesn't know are ignored.

**One problem anywhere drops the whole list.** A region the tablet can't
trust could send an action for the wrong thing, so it doesn't keep the rest.
The image is still shown and the problem is written to the app's log; a bad
`taps` costs the taps, never the image. For the same reason, a list with taps
but a missing or invalid `tap_screen` is dropped.

### What the tablet does with them

- The regions are invisible. They are laid over the picture the way it is
  painted on screen (stretched, fitted or cropped), so they follow it in
  landscape and portrait, and a part of a region that lies outside the visible
  image is cut off.
- **Taps belong to one image.** The app keeps the taps of the latest image it
  fetched, in memory, and sends them only when that exact image is the one on
  screen. An earlier image from history, a newer image that came without taps,
  or the cached image after a restart has no regions. If the server changes
  in Settings, the cached image keeps its regions until a new one arrives.
  A new image closes a sheet that is only waiting for a choice (including a
  confirm question); one that is running an action or showing its answer stays
  until it is closed.
- The regions are also hidden while the Menu, Settings, Today or the
  brightness schedule is open, and opening one of them closes the sheet.
- Tapping a region opens a sheet with the region's `title` and a button per
  action, and a Close button. An action with a `confirm` asks its question
  first, with **Yes, <label>** and **Cancel**. While an action runs every
  button except Close is off and the running one shows `…`. The answer from
  the server is shown in the sheet: after a success the actions go and only
  Close is left; after a failure they stay, so it can be tried again.
- Everything the server sends is drawn as plain text, so markup in a title,
  label or question is shown as typed.
- When an action succeeds, or the gateway says the screen has changed (it
  refused the action, or no longer offers it), the app fetches the screen
  again. That fetch does not move the playlist on; it is the same as
  **Refresh now**.

### How an action reaches the server

Actions go through the same gateway as [Today](TODAY.md), so they need Today
to be set up (`today.json`: the gateway URL, its proxy and token). The tablet
sends

```
POST /widgets/{widget}/actions/{action}
{"key": "<key>", "screen": "<tap_screen>"}
```

to the gateway with the bearer token, and words the answer exactly as Today
does for its own actions (see the tables in [TODAY.md](TODAY.md)). The gateway
is what checks that the widget, key and screen exist and that this tablet may
run the action. Without a gateway the sheet says "Actions need the Today
gateway configured" and nothing is sent.

Only one action runs at a time. A second tap while one is in flight is
answered "Another action is still running" and never sent. A request that
times out is reported as unknown ("Unknown — check <title> in its app") and is
never retried: it may have run.

### Messages

| Message | Direction | Payload |
|---------|-----------|---------|
| 102 | backend → QML | the image message: `{"path", "cached", "saved_at", "taps", "tap_screen"}`. `taps` is always a list (empty, never null) and `tap_screen` is `""` unless the image shown is the one the taps arrived with |
| 20 | QML → backend | `{"widget", "key", "action", "screen"}` and optionally `"title"` |
| 111 | backend → QML | `{"ok", "message", "outcome", "refresh"}` |

A message 20 that is malformed (a widget, action or screen that doesn't match
the patterns above, an empty key, a key or title over 500 characters) is
answered on 111 with "bad action request" and sends nothing. `outcome` is the
gateway's own word for what happened, or one the app works out (`ok`,
`refused`, `denied`, `unknown`, `error`); it is `busy` when another action is
in flight and empty when the request was turned away before it was sent.

## The proxy setting

`proxy` in `/home/root/.config/trmnl-remarkable/config.json` makes the app send
every request to the BYOS server, the display call and the image downloads,
through a local forwarding proxy such as the one `tailscaled` can run on the
tablet:

```json
{
  "base_url": "http://byos.example.ts.net:3000",
  "proxy": "http://127.0.0.1:1055"
}
```

- It must be `http://` to a loopback host (`127.0.0.1`, `::1` or `localhost`)
  with a port, and nothing else: no path, query, credentials or fragment. The
  proxy is meant to be a forwarder running on the tablet itself; a remote one
  is refused.
- It has no control in Settings. Saving Settings keeps it. Remove it from the
  file to go back to direct connections.
- Without `proxy`, nothing changes: the server URL must be HTTPS, or HTTP to a
  loopback mock on the tablet itself.

### Plain HTTP on a tailnet

A tailnet already encrypts and authenticates the traffic between the tablet
and the server, so with a proxy configured the server URL (and image URLs) may
be plain `http://` if the host is:

- a MagicDNS name ending in `.ts.net`, or
- an IPv4 address in `100.64.0.0/10`.

The proxy is what carries the request over the tailnet; the hop the tablet
makes by itself stays on the device. Without `proxy` this rule does not apply,
and an `http://` tailnet address is refused like any other. HTTPS is always
accepted, with or without a proxy.

The rule is checked on the server URL, on every image URL, and on every
redirect an image download follows.

## The access token on image downloads

The Device API key is sent as the `access-token` header on calls to the
server. For image downloads it is sent only when the image is on the **same
origin** as the configured server URL: the same scheme, host and port (so
`https://byos.example` and `https://byos.example:443` are the same origin). It
is never sent to another host, and a redirect that leaves the origin has the
header removed before it is followed.

This lets a server keep its rendered images behind the same key as the API
without leaking the key to a CDN or any other host an image URL may point to.
A server that serves images without authentication is unaffected.
