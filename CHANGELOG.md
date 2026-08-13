# Changelog

## 2.4.0 - 2026-08-12

### Fixed

- The on-screen keyboard could not be dismissed in Settings
  ([#9](https://github.com/usetrmnl/trmnl-remarkable/issues/9)). Tapping a text
  field opened the reMarkable keyboard, which has no key that closes it, and
  nothing on the page took focus away again, so it covered the lower half of
  Settings until the page was closed. A **Hide keyboard** button now appears
  beside the close button while a field is being edited, the keyboard's Enter
  key closes it, and leaving Settings, Controls, or the brightness schedule
  closes it as well.

### Documentation

- The README records the measured battery life on a Paper Pro with the front
  light off.

### Validation

- Passed QML lint, a clean app load, and a focus-driven check of the dismissal
  path against desktop Qt
- Not yet exercised against the reMarkable on-screen keyboard on hardware; see
  `docs/validation/v2.4.md`

## 2.3.0 - 2026-08-11

### Added

- **reMarkable 1 and reMarkable 2 support.** Both are 32-bit ARM, so the
  release now carries a second complete build: an armv7 backend, the arm32
  XOVI runtime, and the arm32 AppLoad extension and qtfb shims. The installer
  identifies the tablet and uploads only the half of the payload that matches
  it, so the transfer stays the size it was.
- The backend detects the panel it is running on from
  `/sys/devices/soc0/machine`, falling back to the device-tree model, and
  reports it to the app and to diagnostics. The reMarkable 1 reports two
  different machine names depending on production batch; both are recognised.
- Dithering now targets the panel that is present. On the reMarkable 1 and 2
  that is the 16-level grey ramp, which converts a colour dashboard to grey by
  diffusing the error instead of flattening it.

### Changed

- The front-light controls, the weekly brightness schedule, and the
  restore-brightness setting are hidden on the reMarkable 1 and 2, which have no
  front light, rather than shown permanently disabled. The Settings page names
  the detected panel and its resolution.
- The `model` header sent to TRMNL and BYOS servers now reports the tablet the
  app is actually running on instead of always claiming a Paper Pro.
- The installer, the device installer, and the runtime installer share one
  device table. Firmware windows are per device: 3.26.x and 3.27.x for the
  Paper Pro and reMarkable 2, and 3.20.x through 3.27.x for the reMarkable 1,
  which no longer receives releases.
- `device/test-on-device.sh` skips the front-light check on devices without one.

### Validation

- Passed Go formatting, tests, and vet checks
- Completed both device cross-builds (arm64 and armv7)
- Not exercised on reMarkable 1 or reMarkable 2 hardware; see
  `docs/validation/v2.3.md`

## 2.2.2 - 2026-08-09

### Changed

- Windows releases are now signed with Azure Trusted Signing under a publicly
  trusted certificate, replacing the self-signed certificate used through
  2.2.1. Windows no longer reports an unknown publisher.

## 2.2.1 - 2026-08-07

### Fixed

- Completed the full-screen weekly brightness grid opened by the Controls
  button, including drag selection, the exact day/time summary, the bottom
  brightness slider, default-cell clearing, and save/discard actions.
- Normalized the backend message constants with Go formatting.

### Changed

- The release workflow now signs with Azure Trusted Signing when the repository
  is configured for it, falling back to a purchased certificate and then to the
  self-signed certificate it has always used. See `docs/code-signing.md`.

### Validation

- Passed Go formatting, tests, and vet checks
- Passed QML linting
- Completed the ARM64 build, resource compilation, and bundle validation

## 2.2.0 - 2026-08-07

### Added

- **Weekly brightness schedule.** A new controls-page editor uses a
  Monday-first grid of half-hour cells. Drag across the days and times to
  select a range, then set its front-light level with the brightness slider.
- The editor shows the exact selected days, time range, and percentage, and
  performs a full e-ink refresh after selections and slider changes.
- Scheduled brightness is applied at startup, after wake, when saved, and at
  every half-hour boundary. Unassigned cells continue using the default
  brightness.

### Changed

- Enabling the weekly schedule switches off reMarkable system-brightness mode
  so scheduled front-light levels can take effect.
- The on-device configuration now persists the complete weekly schedule while
  remaining compatible with existing configuration files.

### Validation

- Passed Go formatting, tests, and vet checks
- Passed QML linting
- Completed the ARM64 build, resource compilation, and bundle validation

## 2.1.1 - 2026-08-05

## Repository and packaging improvements

This maintenance release reorganizes the repository while preserving full
compatibility with existing installations.

### Changed

- Moved on-device scripts and configuration into `device/`
- Moved contributing, security, support, and conduct documents into `.github/`
- Updated CI, documentation links, and repository architecture documentation
- Added explicit archive path rewriting so device installation paths remain unchanged

### Compatibility

The packaged device layout is identical to v2.1.0. Existing absolute paths for
installation, recovery, diagnostics, testing, and uninstall remain unchanged.

### Validation

- Passed Go formatting, tests, and native/ARM64 vet checks
- Passed ShellCheck and QML linting
- Completed the full release build
- Confirmed the archive contains the same 20 paths as v2.1.0
- Tested installation, XOVI activation, stock-interface recovery, and uninstall
  on a reMarkable Paper Pro running firmware 3.27.3.0

## 2.1.0 - 2026-08-05

Validated on a reMarkable Paper Pro running 3.27.3.0; see
[docs/validation/v2.1.md](docs/validation/v2.1.md).

### Added

- **Quiet hours.** Scheduled refreshes pause inside a window you choose, so the
  tablet stops waking overnight. The dashboard stays on screen and manual
  refresh still works.
- **Battery saver.** Below 20% and off the charger, the refresh interval
  stretches up to 4x, capped at six hours.
- **Gradient smoothing.** Optional Floyd-Steinberg dithering to the colour
  panel's palette removes the banding that dashboards drawn for bright screens
  produce. Text and flat colour are left alone, and the palette is overridable
  via `dither_palette` in `config.json`.
- **Update check.** Off by default. When enabled, the tablet asks GitHub once a
  day whether a newer release exists and says so in Settings.
- **Wi-Fi signal reporting.** `readRSSI` previously always returned nothing, so
  the `rssi` header was never sent. It now reads the live signal strength.
- **Installer remembers each tablet's SSH key** and reports on later runs
  whether it matches, changed, or has not been seen before. A changed key blocks
  the install until you explicitly accept it.
- **Installer shows real progress** during an install instead of an indefinite
  spinner, streaming each stage and per-file upload.

### Fixed

- **Reactivate after reboot could silently do nothing.** It backgrounded the
  XOVI start script and returned immediately, so the closing SSH session could
  kill the job before it ran, while still reporting success. `install.sh` ended
  the same way, so a fresh install could finish with the extension runtime not
  running. Both now detach with `setsid` and wait for the injection to appear
  before reporting. If it still does not start, the install is kept and the
  installer tells you to use Reactivate rather than rolling back a good bundle.
- Fix the dashboard freezing on the first inverted screen. Inverted renders are
  now named after their source, so scheduled refreshes and **Previous** update
  the display while **Dark / invert image** is enabled.
- Read battery percentage, charge status, and voltage from the same power supply.
  The Paper Pro exposes a second `type=Battery` device with no capacity file and
  marker accessories as wireless supplies.
- Guard the front-light device against concurrent access from the AppLoad
  receive loop, the refresh scheduler and the shutdown path.
- Redact signed image-URL query parameters from on-screen errors, refresh
  history and the log file.
- Bound every installer SSH operation with a timeout so an unresponsive tablet
  can no longer hang the installer and its operation lock indefinitely.
- Reject non-loopback `Host` headers in the installer and compare its CSRF token
  in constant time.
- Report a real message when a device script fails silently instead of an empty
  installer error.
- Keep only the newest app-bundle and source backups on the tablet so repeated
  reinstalls cannot fill `/home/root`.
- Accept an unquoted `IMG_VERSION` in the guided installer's firmware check,
  matching `install.sh` and the installer's own probe.
- Wait for Xochitl to reappear in `/proc` before verifying recovery, so a slow
  restart can no longer report a false success.
- Resolve the packaged validation record from `VERSION` instead of a hard-coded
  filename, and fail packaging when it is missing.
- Add SOCK_SEQPACKET protocol unit tests, render-path tests, error-redaction
  tests, dithering, update-check, quiet-hours, known-host, installer CSRF/Host
  tests, and a Windows CI job.

### Changed

- Pin the SHA-256 of the three redistributed upstream licence files instead of
  trusting them at download time.
- Derive each dependency's licence from its own file when generating the SBOM
  rather than assuming one for every module.
- The front-light watchdog polls every ten seconds instead of every two.
- Documentation moved under `docs/`; the marketing and research files were
  removed. The README was rewritten.

## 2.0.0 - 2026-08-05

- Redesign the Windows installer as a responsive e-ink-inspired three-step
  experience with paper/ink themes, clearer device status, safer recovery
  controls, accessible focus states, and improved small-screen behavior.
- Detect charging during battery tests, invalidate misleading projections, and
  require a 10% discharge window before estimating runtime.
- Clarify the recommended Paper Pro power configuration: Auto-sleep and Light
  sleep enabled, Auto power-off disabled, and longer refresh intervals for
  substantially better dashboard runtime.
- Add a research-backed media and launch kit with positioning, limitations,
  outreach copy, asset guidance, and a reusable deep-research brief.
- Add a documented self-signed Authenticode path for community builds while
  retaining support for publicly trusted certificates and clearly disclosing
  Windows trust warnings.

## 1.1.0 - 2026-08-04

- Correct Device API metadata to send battery voltage, honor `Retry-After`,
  securely revalidate stable image URLs, and prevent overlay navigation from
  consuming API calls.
- Refuse credential-bearing cross-origin/protocol redirects and non-loopback
  HTTP image URLs; bound and validate downloaded image bytes and dimensions.
- Add graphical post-reboot reactivation, explicit data purge, clearer
  Developer Mode/reset/recovery warnings, and TRMNL BYOD onboarding.
- Add Windows icon/manifest/version metadata, optional Authenticode signing,
  public build provenance, gated release validation, and community launch files.
- Redraw and refresh the dashboard after opening or closing controls, settings,
  diagnostics, and after resume so e-ink overlays cannot remain stuck.
- Request a native full-panel e-ink cleanup after every overlay transition,
  with a black/white fallback cycle when the native signal is unavailable.
- Keep the last dashboard visible while refreshed content loads, avoiding a
  repeated “Waiting for dashboard” flash in Settings.
- Add a persistent, no-terminal battery life test with 15-minute sampling,
  sleep/wake and refresh counts, a trend chart, and projected runtime.
- Add complete front-light controls to the Settings page.
- Add capability-detected RTC wake scheduling for battery-friendly refreshes
  across normal Paper Pro suspend cycles.

All notable changes are documented here. This project follows semantic
versioning.

## 1.0.0 - 2026-08-04

- Initial public-ready Paper Pro release.
- Added a double-click graphical Windows installer with device discovery,
  fingerprint confirmation, payload verification, recovery, and uninstall.
- Added HTTPS-only remote BYOS validation and loopback-only HTTP development.
- Added transactional runtime rollback and shared-runtime-safe uninstall.
- Added reproducible device packaging, release checksums, SBOM, CI, and public
  security/support/contribution documentation.
- Physically verified AppLoad launch, Device API behavior, cache, color display,
  controls, brightness restore, suspend/resume, Wi-Fi loss, crash safety,
  recovery, uninstall, and reboot safety on a Paper Pro running 3.27.3.0.
