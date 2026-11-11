# trmnl-remarkable (fork)

`IvPalmer/trmnl-remarkable`: a public fork of `usetrmnl/trmnl-remarkable` (MIT). Upstream v2.4.0 plus the Today view, BYOS extensions (tap regions, loopback proxy, origin-bound image token) and charging mode. Go backend (`backend/`), QML frontend (`app/ui/`), loaded on the tablet by XOVI/AppLoad.

Where: system / device-surfaces / remarkable / trmnl-remarkable, `~/Dev/system/device-surfaces/remarkable/trmnl-remarkable` (manifest rows `trmnl-remarkable-byos` and `trmnl-remarkable-charging`, part of `remarkable`, whose lead is `remarkable-lead`).

Read the README's "This fork" section first, then `docs/TODAY.md` and `docs/BYOS.md`. The operator's deployment and its state are private and live outside this repo: the operator's elder-brain wiki (`wiki/projects/remarkable.md`) and, on the operator's Mac, `../AGENTS.md` and the companion toolkit `../remarkable-ai`.

## Branches

- `remarkable-ai` is the default branch and what the tablet runs. Work on `claude/<issue>-<topic>` (or `codex/...`) branches, PR into `remarkable-ai`, squash merge.
- `main` tracks upstream `main` (remote `upstream`); keep the fork's patches easy to rebase. Display patches in `app/ui/TRMNL.qml` are marked `remarkable-ai`.

## Build and verify

- `gofmt -l backend installer` prints nothing; `go test ./...` and `go vet ./...` pass (CI also runs `-race`, `qmllint`, `qmltestrunner` on `tests/qml` and the AppLoad harness `tests/appload_harness.py`).
- Tablet build (Paper Pro, arm64): `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -trimpath -o <out>/entry ./backend/cmd/trmnl-remarkable`, plus `rcc --format-version 3 --compress-algo zlib --binary -o <out>/resources.rcc application.qrc` in `app/` (zlib, not zstd: the tablet's Qt 6.8 is not known to read zstd). The operator deploys from the companion toolkit, not with the Windows installer.

## Rules

- Public repo: no device keys, tokens, tailnet or host names, IP addresses or account details in commits, issues or docs.
- A reMarkable OS update can break XOVI/AppLoad; check `docs/compatibility.md` before assuming the app still loads.
