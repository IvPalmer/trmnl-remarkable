#!/bin/sh
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
fail=0
check(){ if "$@"; then echo "PASS: $*"; else echo "FAIL: $*"; fail=1; fi; }
check test -x "$ROOT/dist/trmnl-remarkable-app/backend/entry"
check test -s "$ROOT/dist/trmnl-remarkable-app/resources.rcc"
check test -f /home/root/xovi/exthome/appload/trmnl-remarkable/manifest.json
check systemctl is-active --quiet xochitl
check test "$(stat -c %a /home/root/.config/trmnl-remarkable/config.json 2>/dev/null || echo 600)" = 600
# Only the Paper Pro has a front light; on the reMarkable 1 and 2 its absence is
# expected rather than a failure.
machine=$(cat /sys/devices/soc0/machine 2>/dev/null || true)
if [ "$machine" = "reMarkable Ferrari" ]; then
  # The single quotes intentionally preserve $d for the tablet-side shell.
  # shellcheck disable=SC2016
  check sh -c 'for d in /sys/class/backlight/*; do [ -r "$d/brightness" ] && [ -r "$d/max_brightness" ] && exit 0; done; exit 1'
else
  echo "SKIP: front-light check ($machine has no front light)"
fi
exit "$fail"
