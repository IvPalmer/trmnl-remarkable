package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"time"
	"trmnl-remarkable/backend/internal/config"
	"trmnl-remarkable/backend/internal/today"
)

func TestReadBatteryPercentPrefersSystemBattery(t *testing.T) {
	root := t.TempDir()
	writePowerSupply(t, root, "elants-marker-battery", "Wireless", "0")
	writePowerSupply(t, root, "max1726x_battery", "Battery", "71")
	if got := readBatteryPercentAt(root); got != 71 {
		t.Fatalf("readBatteryPercentAt() = %d, want 71", got)
	}
}

func TestReadBatteryVoltageUsesSystemBatteryMicrovolts(t *testing.T) {
	root := t.TempDir()
	writePowerSupply(t, root, "max1726x_battery", "Battery", "71")
	path := filepath.Join(root, "max1726x_battery", "voltage_now")
	if err := os.WriteFile(path, []byte("3925000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := readBatteryVoltageAt(root); got != "3.925" {
		t.Fatalf("readBatteryVoltageAt() = %q, want 3.925", got)
	}
}

func TestValidateImagePayload(t *testing.T) {
	var encoded bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 1620, 2160))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	if err := validateImagePayload(encoded.Bytes()); err != nil {
		t.Fatalf("valid panel image rejected: %v", err)
	}
	if err := validateImagePayload([]byte("not an image")); err == nil {
		t.Fatal("invalid image accepted")
	}
	encoded.Reset()
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 10001, 1))); err != nil {
		t.Fatal(err)
	}
	if err := validateImagePayload(encoded.Bytes()); err == nil {
		t.Fatal("oversized image dimensions accepted")
	}
}

func TestReadDeviceIDPrefersWiFiMAC(t *testing.T) {
	root := t.TempDir()
	writeInterfaceAddress(t, root, "usb0", "02:00:00:00:00:01")
	writeInterfaceAddress(t, root, "wlan0", "aa:bb:cc:dd:ee:ff")
	if got := readDeviceIDAt(root); got != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("readDeviceIDAt() = %q, want Wi-Fi MAC", got)
	}
}

func TestUntilNextBrightnessSlot(t *testing.T) {
	at := time.Date(2026, 8, 3, 8, 12, 30, 0, time.Local)
	if got, want := untilNextBrightnessSlot(at), 17*time.Minute+30*time.Second; got != want {
		t.Fatalf("untilNextBrightnessSlot() = %v, want %v", got, want)
	}
	at = time.Date(2026, 8, 3, 8, 30, 0, 0, time.Local)
	if got := untilNextBrightnessSlot(at); got != 30*time.Minute {
		t.Fatalf("boundary duration = %v, want 30m", got)
	}
}

// QML's Image ignores a source assignment that repeats the current URL, so
// every distinct cached screen must render to a distinct path.
func TestRenderedViewPathTracksItsSource(t *testing.T) {
	a := &app{dataDir: t.TempDir()}
	cfg := config.Defaults()
	cfg.Invert = true
	first := writeTestScreen(t, a.dataDir, "screen-1000.png")
	second := writeTestScreen(t, a.dataDir, "screen-2000.png")

	firstRender, err := a.renderedView(first, cfg)
	if err != nil {
		t.Fatalf("invert first screen: %v", err)
	}
	secondRender, err := a.renderedView(second, cfg)
	if err != nil {
		t.Fatalf("invert second screen: %v", err)
	}
	if firstRender == secondRender {
		t.Fatalf("both screens rendered to %s; the dashboard would never reload", firstRender)
	}
	repeat, err := a.renderedView(first, cfg)
	if err != nil {
		t.Fatalf("re-invert first screen: %v", err)
	}
	if repeat != firstRender {
		t.Fatalf("re-rendering %s produced %s, want the stable %s", first, repeat, firstRender)
	}
	for _, path := range []string{firstRender, secondRender} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("render %s is missing: %v", path, err)
		}
	}
}

// With no transform enabled the cached file is shown directly, so no render is
// written and nothing has to be pruned later.
func TestRenderedViewReturnsSourceWhenNothingApplies(t *testing.T) {
	a := &app{dataDir: t.TempDir()}
	source := writeTestScreen(t, a.dataDir, "screen-3000.png")
	got, err := a.renderedView(source, config.Defaults())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got != source {
		t.Fatalf("render = %s, want the untouched source %s", got, source)
	}
}

// Dithering and inversion must produce distinct filenames so switching either
// setting reloads the panel.
func TestRenderedViewNamesEncodeTheTransforms(t *testing.T) {
	a := &app{dataDir: t.TempDir()}
	source := writeGradientScreen(t, a.dataDir, "screen-4000.png")

	inverted := config.Defaults()
	inverted.Invert = true
	dithered := config.Defaults()
	dithered.Dither = "auto"
	both := config.Defaults()
	both.Invert, both.Dither = true, "auto"

	seen := map[string]string{}
	for name, cfg := range map[string]config.Config{"invert": inverted, "dither": dithered, "both": both} {
		path, err := a.renderedView(source, cfg)
		if err != nil {
			t.Fatalf("%s render: %v", name, err)
		}
		if previous, clash := seen[path]; clash {
			t.Fatalf("%s and %s both rendered to %s", name, previous, path)
		}
		seen[path] = name
	}
}

func TestBatterySavingRespectsChargingAndThreshold(t *testing.T) {
	a := &app{}
	off := config.Defaults()
	off.BatterySaverPercent = 0
	if saving, _ := a.batterySaving(off); saving {
		t.Fatal("battery saving engaged while disabled")
	}
}

func TestPruneInvertedKeepsNewestRenders(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "inverted.png")
	if err := os.WriteFile(legacy, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for i := range 5 {
		p := filepath.Join(dir, fmt.Sprintf("inverted-screen-%d.png", i))
		if err := os.WriteFile(p, []byte("render"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, time.Unix(int64(1000+i), 0), time.Unix(int64(1000+i), 0)); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	pruneInverted(dir, 2)
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("the pre-2.1 fixed-name render was not removed")
	}
	for i, p := range paths {
		_, err := os.Stat(p)
		if i >= 3 && err != nil {
			t.Fatalf("newest render %s was pruned: %v", p, err)
		}
		if i < 3 && err == nil {
			t.Fatalf("stale render %s survived pruning", p)
		}
	}
}

// Errors reach the tablet screen, the refresh history and the log file, so a
// signed image URL must not survive them.
func TestSafeErrorRedactsCredentialsAndSignedURLs(t *testing.T) {
	signed := errors.New(`Get "https://trmnl-assets.s3.amazonaws.com/screens/abc.png?X-Amz-Signature=deadbeef&X-Amz-Credential=AKIA": dial tcp: i/o timeout`)
	got := safeError(signed)
	for _, secret := range []string{"X-Amz-Signature", "deadbeef", "AKIA"} {
		if strings.Contains(got, secret) {
			t.Fatalf("safeError leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "trmnl-assets.s3.amazonaws.com") {
		t.Fatalf("safeError dropped the origin operators need: %s", got)
	}

	if got := safeError(errors.New(`request failed with access-token abc123`)); strings.Contains(got, "abc123") {
		t.Fatalf("safeError leaked the access token: %s", got)
	}

	plain := errors.New(`Get "https://trmnl.com/api/display": connection refused`)
	if got := safeError(plain); !strings.Contains(got, "https://trmnl.com/api/display") {
		t.Fatalf("safeError mangled a URL that carries no secrets: %s", got)
	}
	if safeError(nil) != "" {
		t.Fatal("safeError(nil) should be empty")
	}
}

func writeGradientScreen(t *testing.T, dir, name string) string {
	t.Helper()
	var encoded bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 24, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 10), G: uint8(y * 10), B: 120, A: 255})
		}
	}
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTestScreen(t *testing.T, dir, name string) string {
	t.Helper()
	var encoded bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeInterfaceAddress(t *testing.T, root, name, address string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "address"), []byte(address+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func writePowerSupply(t *testing.T, root, name, typ, capacity string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "type"), []byte(typ+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "capacity"), []byte(capacity+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestOpenTodayIsOffWithoutItsConfig(t *testing.T) {
	e, problem := openToday(context.Background(), t.TempDir(), func(uint32, string) {})
	if e != nil || problem != "" {
		t.Fatalf("openToday = %v, %q", e, problem)
	}
}

func TestOpenTodayReportsABadConfigWithoutTheToken(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "trmnl-remarkable")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	tok := filepath.Join(dir, "today.token")
	if err := os.WriteFile(tok, []byte("secret value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := json.Marshal(map[string]string{
		"gateway_url": "http://mac.test:8090",
		"proxy":       "http://127.0.0.1:1055",
		"token_file":  tok,
		"timezone":    "America/Sao_Paulo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "today.json"), cfg, 0600); err != nil {
		t.Fatal(err)
	}
	e, problem := openToday(context.Background(), home, func(uint32, string) {})
	if e != nil || problem == "" || strings.Contains(problem, "secret") {
		t.Fatalf("openToday = %v, %q", e, problem)
	}
}

// writeTodayConfig makes a usable Today setup under home and returns the
// token file's path. extra overrides or adds today.json fields.
func writeTodayConfig(t *testing.T, home string, extra map[string]any) string {
	t.Helper()
	dir := filepath.Join(home, ".config", "trmnl-remarkable")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	tok := filepath.Join(dir, "today.token")
	if err := os.WriteFile(tok, []byte("test-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{
		"gateway_url": "http://mac.test:8090",
		"proxy":       "http://127.0.0.1:1",
		"token_file":  tok,
		"timezone":    "America/Sao_Paulo",
	}
	for k, v := range extra {
		fields[k] = v
	}
	cfg, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "today.json"), cfg, 0600); err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestOpenTodayNeedsAKnownSection(t *testing.T) {
	home := t.TempDir()
	writeTodayConfig(t, home, map[string]any{"sections": []string{"nonsense"}})
	e, problem := openToday(context.Background(), home, func(uint32, string) {})
	if e != nil || problem != "today.json: no known sections" {
		t.Fatalf("openToday = %v, %q", e, problem)
	}
}

func TestOpenTodayKeepsTheKnownSectionsAmongUnknownOnes(t *testing.T) {
	home := t.TempDir()
	writeTodayConfig(t, home, map[string]any{"sections": []string{"nonsense", "widgets"}})
	e, problem := openToday(context.Background(), home, func(uint32, string) {})
	if e == nil || problem != "" {
		t.Fatalf("openToday = %v, %q", e, problem)
	}
	if got := e.Current().Sections; len(got) != 1 || got[0].ID != "widgets" {
		t.Fatalf("sections = %+v", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestClearCacheRemovesTodaysCacheWhileTodayIsOff(t *testing.T) {
	home := t.TempDir()
	cacheDir := filepath.Join(home, ".cache", "trmnl-remarkable")
	a := &app{todayCache: todayCachePath(home)}
	writeFile(t, a.todayCache, `{"version":1}`)
	writeFile(t, filepath.Join(cacheDir, ".today-1234"), "interrupted save")
	writeFile(t, filepath.Join(cacheDir, "index.json"), "[]")
	a.clearToday()
	if exists(a.todayCache) || exists(filepath.Join(cacheDir, ".today-1234")) {
		t.Fatal("Today's cache survived Clear cache")
	}
	if !exists(filepath.Join(cacheDir, "index.json")) {
		t.Fatal("Clear cache for Today removed a file that is not Today's")
	}
	a.clearToday() // nothing left: still fine
}

func TestClearCacheRemovesTodaysCacheWhenTodayIsBroken(t *testing.T) {
	home := t.TempDir()
	writeTodayConfig(t, home, map[string]any{"proxy": ""})
	e, problem := openToday(context.Background(), home, func(uint32, string) {})
	if e != nil || problem == "" {
		t.Fatalf("openToday = %v, %q", e, problem)
	}
	a := &app{today: e, todayProblem: problem, todayCache: todayCachePath(home)}
	writeFile(t, a.todayCache, `{"version":1}`)
	a.clearToday()
	if exists(a.todayCache) {
		t.Fatal("Today's cache survived Clear cache")
	}
}

func TestClearCacheGoesThroughTheEngineWhileTodayIsOn(t *testing.T) {
	home := t.TempDir()
	writeTodayConfig(t, home, nil)
	var snapshots int
	e, problem := openToday(context.Background(), home, func(typ uint32, _ string) {
		if typ == today.MsgToday {
			snapshots++
		}
	})
	if e == nil {
		t.Fatalf("openToday: %q", problem)
	}
	a := &app{today: e, todayCache: todayCachePath(home)}
	writeFile(t, a.todayCache, `{"version":1}`)
	a.clearToday()
	if exists(a.todayCache) || snapshots != 1 {
		t.Fatalf("cache present = %t, snapshots = %d", exists(a.todayCache), snapshots)
	}
}

func TestCleanupTempsRemovesAnInterruptedTodaySave(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".today-98765"), "half a cache")
	writeFile(t, filepath.Join(dir, "today.json"), `{"version":1}`)
	cleanupTemps(dir)
	if exists(filepath.Join(dir, ".today-98765")) {
		t.Fatal("the interrupted save's temp file survived")
	}
	if !exists(filepath.Join(dir, "today.json")) {
		t.Fatal("cleanupTemps removed the cache itself")
	}
}

// decodeRefusal reads a message 110 and fails unless it has the whole field set.
func decodeRefusal(t *testing.T, reply string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(reply), &m); err != nil {
		t.Fatalf("reply %q: %v", reply, err)
	}
	for _, k := range []string{"ok", "section", "action", "key", "message"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("reply %q has no %q", reply, k)
		}
	}
	if m["ok"] != false {
		t.Fatalf("reply %q is not a refusal", reply)
	}
	return m
}

func TestTodayActRefusalsCarryTheWholeReply(t *testing.T) {
	request := `{"section":"widgets","rev":1790000000000,"action":"tick","key":"demo.tasks t:41"}`
	off := &app{}
	m := decodeRefusal(t, off.todayAct(request))
	if m["section"] != "widgets" || m["action"] != "tick" || m["key"] != "demo.tasks t:41" || m["message"] != "Today is not set up" {
		t.Fatalf("off: %v", m)
	}
	broken := &app{todayProblem: "today.json: proxy is required"}
	m = decodeRefusal(t, broken.todayAct(request))
	if m["message"] != "today.json: proxy is required" || m["section"] != "widgets" {
		t.Fatalf("broken: %v", m)
	}
	m = decodeRefusal(t, off.todayAct("{"))
	if m["section"] != "" || m["action"] != "" || m["key"] != "" || m["message"] != "Today is not set up" {
		t.Fatalf("off, unreadable: %v", m)
	}

	home := t.TempDir()
	writeTodayConfig(t, home, nil)
	e, problem := openToday(context.Background(), home, func(uint32, string) {})
	if e == nil {
		t.Fatalf("openToday: %q", problem)
	}
	on := &app{today: e}
	m = decodeRefusal(t, on.todayAct("not json"))
	if m["section"] != "" || m["message"] != "bad action request" {
		t.Fatalf("on, unreadable: %v", m)
	}
	// A field of the wrong type fails the decode; what did decode is echoed.
	m = decodeRefusal(t, on.todayAct(`{"section":"widgets","rev":"x","action":"tick","key":"k"}`))
	if m["section"] != "widgets" || m["key"] != "k" || m["message"] != "bad action request" {
		t.Fatalf("on, bad rev: %v", m)
	}
}

func TestTodayActPassesAGoodRequestToTheEngine(t *testing.T) {
	home := t.TempDir()
	writeTodayConfig(t, home, nil)
	var typ uint32
	var payload string
	e, problem := openToday(context.Background(), home, func(t uint32, p string) { typ, payload = t, p })
	if e == nil {
		t.Fatalf("openToday: %q", problem)
	}
	a := &app{today: e}
	if reply := a.todayAct(`{"section":"nope","rev":1,"action":"tick","key":"k"}`); reply != "" {
		t.Fatalf("a good request was refused: %s", reply)
	}
	// The engine itself refused (no such section) and said so in message 110.
	if typ != today.MsgActResult || decodeRefusal(t, payload)["message"] != "unknown section" {
		t.Fatalf("engine got %d %s", typ, payload)
	}
}
