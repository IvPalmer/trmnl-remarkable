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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"time"
	"trmnl-remarkable/backend/internal/cache"
	"trmnl-remarkable/backend/internal/config"
	"trmnl-remarkable/backend/internal/protocol"
	"trmnl-remarkable/backend/internal/today"
	"trmnl-remarkable/backend/internal/trmnl"
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
	e, gw, problem := openToday(context.Background(), t.TempDir(), func(uint32, string) {})
	if e != nil || gw != nil || problem != "" {
		t.Fatalf("openToday = %v, %v, %q", e, gw, problem)
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
	e, gw, problem := openToday(context.Background(), home, func(uint32, string) {})
	if e != nil || gw != nil || problem == "" || strings.Contains(problem, "secret") {
		t.Fatalf("openToday = %v, %v, %q", e, gw, problem)
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
	e, gw, problem := openToday(context.Background(), home, func(uint32, string) {})
	if e != nil || gw != nil || problem != "today.json: no known sections" {
		t.Fatalf("openToday = %v, %v, %q", e, gw, problem)
	}
}

func TestOpenTodayKeepsTheKnownSectionsAmongUnknownOnes(t *testing.T) {
	home := t.TempDir()
	writeTodayConfig(t, home, map[string]any{"sections": []string{"nonsense", "widgets"}})
	e, gw, problem := openToday(context.Background(), home, func(uint32, string) {})
	if e == nil || gw == nil || problem != "" {
		t.Fatalf("openToday = %v, %v, %q", e, gw, problem)
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
	e, _, problem := openToday(context.Background(), home, func(uint32, string) {})
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
	e, _, problem := openToday(context.Background(), home, func(typ uint32, _ string) {
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
	e, _, problem := openToday(context.Background(), home, func(uint32, string) {})
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
	e, _, problem := openToday(context.Background(), home, func(t uint32, p string) { typ, payload = t, p })
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

// outbox stands in for the AppLoad connection: everything the backend sends
// lands here, in order.
type outbox struct {
	mu   sync.Mutex
	msgs []outMsg
	on   func(typ uint32)
}

type outMsg struct {
	typ     uint32
	payload string
}

func (o *outbox) emit(typ uint32, payload string) {
	if o.on != nil {
		o.on(typ)
	}
	o.mu.Lock()
	o.msgs = append(o.msgs, outMsg{typ, payload})
	o.mu.Unlock()
}

func (o *outbox) of(typ uint32) []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []string
	for _, m := range o.msgs {
		if m.typ == typ {
			out = append(out, m.payload)
		}
	}
	return out
}

// waitFor polls cond for a few seconds; it is for work started on a goroutine.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// sentImage is message 102 as the view reads it.
type sentImage struct {
	Path      string      `json:"path"`
	Cached    bool        `json:"cached"`
	Taps      []trmnl.Tap `json:"taps"`
	TapScreen string      `json:"tap_screen"`
}

func lastImage(t *testing.T, o *outbox) (sentImage, string) {
	t.Helper()
	all := o.of(msgImage)
	if len(all) == 0 {
		t.Fatal("no message 102 was sent")
	}
	raw := all[len(all)-1]
	var im sentImage
	if err := json.Unmarshal([]byte(raw), &im); err != nil {
		t.Fatalf("102 %q: %v", raw, err)
	}
	return im, raw
}

func sampleTaps() []trmnl.Tap {
	return []trmnl.Tap{{X: 10, Y: 20, W: 300, H: 40, Widget: "demo.tasks", Key: "t:41", Title: "Pay rent",
		Actions: []trmnl.TapAction{{ID: "tick", Label: "Done"}, {ID: "drop", Label: "Drop", Confirm: "Drop it?"}}}}
}

func TestSendImageCarriesTapsOnlyForTheirOwnImage(t *testing.T) {
	o := &outbox{}
	a := &app{emit: o.emit}
	one := cache.Entry{Path: "/cache/screen-1.png"}
	two := cache.Entry{Path: "/cache/screen-2.png"}
	a.setTaps(one.Path, sampleTaps(), "page1")

	a.sendImage(one, false)
	im, _ := lastImage(t, o)
	if len(im.Taps) != 1 || im.Taps[0].Widget != "demo.tasks" || im.Taps[0].Key != "t:41" || im.TapScreen != "page1" ||
		len(im.Taps[0].Actions) != 2 || im.Taps[0].Actions[1].Confirm != "Drop it?" {
		t.Fatalf("the image's own taps: %+v", im)
	}

	// Another image (the cached start-up screen, Previous) has no taps, and
	// says so with [] and no screen, not null.
	a.sendImage(two, true)
	im, raw := lastImage(t, o)
	if len(im.Taps) != 0 || im.TapScreen != "" || !strings.Contains(raw, `"taps":[]`) || !strings.Contains(raw, `"tap_screen":""`) {
		t.Fatalf("another image: %s", raw)
	}

	// The same image again from the cache (settings save): its taps are still known.
	a.sendImage(one, true)
	if im, _ := lastImage(t, o); len(im.Taps) != 1 || im.TapScreen != "page1" || !im.Cached {
		t.Fatalf("the same image from the cache: %+v", im)
	}

	// A newer image without taps replaces them: the old image shows none.
	a.setTaps(two.Path, nil, "")
	a.sendImage(one, true)
	if im, raw := lastImage(t, o); len(im.Taps) != 0 || im.TapScreen != "" || !strings.Contains(raw, `"taps":[]`) {
		t.Fatalf("after a newer image: %s", raw)
	}
}

// displayServer is a BYOS server: /api/display answers with the current
// image and taps, /img/* serves PNGs with an ETag.
type displayServer struct {
	*httptest.Server
	mu        sync.Mutex
	image     string // current image file name
	taps      string // raw JSON for "taps" ("" omits the field)
	tapScreen string
	pngs      map[string][]byte
}

func newDisplayServer(t *testing.T) *displayServer {
	t.Helper()
	d := &displayServer{pngs: map[string][]byte{}}
	for i, name := range []string{"a.png", "b.png"} {
		img := image.NewNRGBA(image.Rect(0, 0, 4+i, 4))
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		d.pngs[name] = buf.Bytes()
	}
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/display"):
			resp := map[string]any{"status": 0, "image_url": d.URL + "/img/" + d.image, "filename": d.image, "refresh_rate": 60}
			if d.taps != "" {
				resp["taps"] = json.RawMessage(d.taps)
				resp["tap_screen"] = d.tapScreen
			}
			_ = json.NewEncoder(w).Encode(resp)
		case strings.HasPrefix(r.URL.Path, "/img/"):
			name := strings.TrimPrefix(r.URL.Path, "/img/")
			etag := `"` + name + `"`
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
			_, _ = w.Write(d.pngs[name])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(d.Close)
	return d
}

func (d *displayServer) show(image, taps, screen string) {
	d.mu.Lock()
	d.image, d.taps, d.tapScreen = image, taps, screen
	d.mu.Unlock()
}

const oneTapJSON = `[{"x":10,"y":20,"w":300,"h":40,"widget":"demo.tasks","key":"t:41","title":"Pay rent","actions":[{"id":"tick","label":"Done"}]}]`

func TestFetchSendsTapsOnlyWithTheImageTheyCameWith(t *testing.T) {
	srv := newDisplayServer(t)
	o := &outbox{}
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.BaseURL = srv.URL
	cfg.DeviceID = "AA:BB:CC:DD:EE:FF"
	a := &app{cfg: cfg, client: trmnl.New(), cache: cache.Store{Dir: filepath.Join(dir, "cache")},
		historyPath: filepath.Join(dir, "history.json"), batteryPath: filepath.Join(dir, "battery.json"),
		ctx: context.Background(), emit: o.emit}

	fetch := func(advance bool) sentImage {
		t.Helper()
		if ok, _ := a.fetch(trigger{advance: advance, reason: "test"}); !ok {
			t.Fatalf("fetch failed: %v", o.of(msgError))
		}
		im, _ := lastImage(t, o)
		return im
	}

	// A new image with its taps.
	srv.show("a.png", oneTapJSON, "page1")
	first := fetch(true)
	if len(first.Taps) != 1 || first.Taps[0].Key != "t:41" || first.TapScreen != "page1" || first.Cached {
		t.Fatalf("new image: %+v", first)
	}

	// The server confirms the same image (304) and sends fresh taps for it.
	srv.show("a.png", strings.Replace(oneTapJSON, "t:41", "t:42", 1), "page1")
	same := fetch(false)
	if same.Path != first.Path || len(same.Taps) != 1 || same.Taps[0].Key != "t:42" {
		t.Fatalf("unchanged image: %+v (first %+v)", same, first)
	}

	// A different image without taps, then a cached start-up style resend of
	// the earlier one: neither carries the taps of the image before it.
	srv.show("b.png", "", "")
	next := fetch(true)
	if next.Path == first.Path || len(next.Taps) != 0 || next.TapScreen != "" {
		t.Fatalf("image without taps: %+v", next)
	}
	entries, err := a.cache.Entries()
	if err != nil || len(entries) != 2 {
		t.Fatalf("cache entries = %v, %v", entries, err)
	}
	a.showPrevious()
	prev, raw := lastImage(t, o)
	if prev.Path != first.Path || len(prev.Taps) != 0 || prev.TapScreen != "" || !strings.Contains(raw, `"taps":[]`) {
		t.Fatalf("previous image: %s", raw)
	}

	// Bad taps cost only the taps: the image still shows, with none.
	srv.show("a.png", `[{"x":"nope"}]`, "page1")
	bad := fetch(true)
	if len(bad.Taps) != 0 || bad.TapScreen != "" {
		t.Fatalf("bad taps: %+v", bad)
	}
}

// gatewayStub is the Today gateway behind the proxy the app's gateway uses.
type gatewayStub struct {
	*httptest.Server
	mu      sync.Mutex
	reqs    []gatewayReq
	status  int
	body    string
	entered chan struct{} // when set, each request signals here ...
	release chan struct{} // ... and waits for this to be closed
}

type gatewayReq struct{ method, uri, auth, body string }

func newGatewayStub(t *testing.T, status int, body string) *gatewayStub {
	t.Helper()
	g := &gatewayStub{status: status, body: body}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		g.mu.Lock()
		g.reqs = append(g.reqs, gatewayReq{r.Method, r.RequestURI, r.Header.Get("Authorization"), string(b)})
		status, body, entered, release := g.status, g.body, g.entered, g.release
		g.mu.Unlock()
		if entered != nil {
			entered <- struct{}{}
			select { // a bounded wait, so a missing guard fails the test instead of hanging it
			case <-release:
			case <-time.After(2 * time.Second):
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *gatewayStub) requests() []gatewayReq {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]gatewayReq(nil), g.reqs...)
}

func (g *gatewayStub) respond(status int, body string) {
	g.mu.Lock()
	g.status, g.body = status, body
	g.mu.Unlock()
}

// tapApp is an app whose Today gateway goes through stub (the gateway's proxy).
func tapApp(t *testing.T, stub *gatewayStub) (*app, *outbox) {
	t.Helper()
	gw, err := today.NewGateway(today.Config{GatewayURL: "http://mac.test:8090", Proxy: stub.URL,
		TokenFile: "/unused", Timezone: "UTC"}, "tok")
	if err != nil {
		t.Fatal(err)
	}
	o := &outbox{}
	return &app{gateway: gw, ctx: context.Background(), triggers: make(chan trigger, 4), emit: o.emit}, o
}

const goodTap = `{"widget":"demo.tasks","key":"t:41","action":"tick","screen":"page1","title":"Pay rent"}`

func decodeTapResult(t *testing.T, reply string) today.TapResult {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(reply), &raw); err != nil {
		t.Fatalf("reply %q: %v", reply, err)
	}
	for _, k := range []string{"ok", "message", "outcome", "refresh"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("reply %q has no %q", reply, k)
		}
	}
	var res today.TapResult
	if err := json.Unmarshal([]byte(reply), &res); err != nil {
		t.Fatal(err)
	}
	return res
}

func queued(a *app) (trigger, bool) {
	select {
	case tr := <-a.triggers:
		return tr, true
	default:
		return trigger{}, false
	}
}

func TestMessage20RunsTheActionAnswersOn111AndRefreshesWithoutAdvancing(t *testing.T) {
	stub := newGatewayStub(t, 200, `{"ok":true,"message":"Ticked","outcome":"done","refresh":true}`)
	a, o := tapApp(t, stub)
	var waiting int
	o.on = func(typ uint32) {
		if typ == msgTapResult {
			waiting = len(a.triggers) // the answer goes out before the refresh is queued
		}
	}
	a.handle(protocol.Message{Type: msgTapAct, Contents: goodTap})
	waitFor(t, "message 111", func() bool { return len(o.of(msgTapResult)) == 1 })
	waitFor(t, "the refresh", func() bool { return len(a.triggers) == 1 })

	reqs := stub.requests()
	if len(reqs) != 1 || reqs[0].method != "POST" || reqs[0].uri != "http://mac.test:8090/widgets/demo.tasks/actions/tick" ||
		reqs[0].auth != "Bearer tok" {
		t.Fatalf("gateway saw %+v", reqs)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(reqs[0].body), &body); err != nil || len(body) != 2 || body["key"] != "t:41" || body["screen"] != "page1" {
		t.Fatalf("gateway body %q: %v", reqs[0].body, err)
	}
	res := decodeTapResult(t, o.of(msgTapResult)[0])
	if !res.OK || res.Message != "Ticked" || res.Outcome != "done" || !res.Refresh {
		t.Fatalf("111 = %+v", res)
	}
	tr, _ := queued(a)
	if tr.advance || tr.reason != "after action" || waiting != 0 {
		t.Fatalf("refresh = %+v (queue length at the answer: %d)", tr, waiting)
	}
}

func TestATapOutcomeRefreshesOnlyWhenTheScreenIsStaleOrChanged(t *testing.T) {
	for _, c := range []struct {
		name    string
		status  int
		body    string
		ok      bool
		outcome string
		message string
		refresh bool
	}{
		{"done, nothing to redraw", 200, `{"ok":true,"message":"Ticked"}`, true, "ok", "Ticked", true},
		{"refused: the screen is stale", 409, `{"message":"item gone","outcome":"refused"}`, false, "refused", "item gone", true},
		{"refused in a 200", 200, `{"ok":false,"message":"nope","outcome":"refused"}`, false, "refused", "nope", true},
		{"denied", 403, `{"message":"not on this screen","outcome":"denied"}`, false, "denied", "not on this screen", false},
		{"unknown: the app timed out", 504, `{"outcome":"unknown"}`, false, "unknown", "Unknown — check Pay rent in its app", false},
		{"not authorised", 401, `{"error":"bad token"}`, false, "error", "Tablet not authorised. Run rm-today-setup.", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			stub := newGatewayStub(t, c.status, c.body)
			a, o := tapApp(t, stub)
			a.runTapAct(goodTap)
			res := decodeTapResult(t, o.of(msgTapResult)[0])
			if res.OK != c.ok || res.Outcome != c.outcome || res.Message != c.message {
				t.Fatalf("111 = %+v", res)
			}
			tr, got := queued(a)
			if got != c.refresh || (got && (tr.advance || tr.reason != "after action")) {
				t.Fatalf("refresh queued = %v %+v, want %v", got, tr, c.refresh)
			}
			if len(stub.requests()) != 1 {
				t.Fatalf("the gateway saw %d requests", len(stub.requests()))
			}
		})
	}
}

func TestATapActionThatFailsToReachTheGatewayDoesNotRefresh(t *testing.T) {
	stub := newGatewayStub(t, 200, `{}`)
	a, o := tapApp(t, stub)
	stub.Close() // the proxy is gone: tailscaled is not running
	a.runTapAct(goodTap)
	res := decodeTapResult(t, o.of(msgTapResult)[0])
	if res.OK || res.Outcome != "error" || res.Message != "Tailscale isn't running on the tablet" || res.Refresh {
		t.Fatalf("111 = %+v", res)
	}
	if _, got := queued(a); got {
		t.Fatal("a refresh was queued for an action that never left the tablet")
	}
}

func TestMessage20WithABadRequestSendsNothing(t *testing.T) {
	long := strings.Repeat("k", 501)
	for name, req := range map[string]string{
		"not json":          `{`,
		"empty":             `{}`,
		"no key":            `{"widget":"demo.tasks","action":"tick","screen":"page1"}`,
		"key too long":      `{"widget":"demo.tasks","key":"` + long + `","action":"tick","screen":"page1"}`,
		"title too long":    `{"widget":"demo.tasks","key":"k","action":"tick","screen":"page1","title":"` + long + `"}`,
		"no widget":         `{"key":"k","action":"tick","screen":"page1"}`,
		"widget not app.id": `{"widget":"tasks","key":"k","action":"tick","screen":"page1"}`,
		"widget upper case": `{"widget":"Demo.tasks","key":"k","action":"tick","screen":"page1"}`,
		"widget path":       `{"widget":"demo.tasks/../x","key":"k","action":"tick","screen":"page1"}`,
		"action with space": `{"widget":"demo.tasks","key":"k","action":"do it","screen":"page1"}`,
		"action path":       `{"widget":"demo.tasks","key":"k","action":"../x","screen":"page1"}`,
		"no action":         `{"widget":"demo.tasks","key":"k","screen":"page1"}`,
		"no screen":         `{"widget":"demo.tasks","key":"k","action":"tick"}`,
		"screen with dash":  `{"widget":"demo.tasks","key":"k","action":"tick","screen":"page-1"}`,
		"key is a number":   `{"widget":"demo.tasks","key":5,"action":"tick","screen":"page1"}`,
	} {
		t.Run(name, func(t *testing.T) {
			stub := newGatewayStub(t, 200, `{"ok":true}`)
			a, o := tapApp(t, stub)
			a.runTapAct(req)
			res := decodeTapResult(t, o.of(msgTapResult)[0])
			if res.OK || res.Message != "bad action request" || res.Outcome != "" || res.Refresh {
				t.Fatalf("111 = %+v", res)
			}
			if n := len(stub.requests()); n != 0 {
				t.Fatalf("the gateway saw %d requests", n)
			}
			if _, got := queued(a); got {
				t.Fatal("a refresh was queued")
			}
		})
	}
}

func TestMessage20WithoutAGatewayExplainsAndSendsNothing(t *testing.T) {
	stub := newGatewayStub(t, 200, `{"ok":true}`) // never reached: the app has no gateway
	for name, c := range map[string]struct{ problem, want string }{
		"no Today config": {"", "Actions need the Today gateway configured"},
		"a broken config": {"today.json: proxy is required", "Actions need the Today gateway configured (today.json: proxy is required)"},
	} {
		t.Run(name, func(t *testing.T) {
			o := &outbox{}
			a := &app{todayProblem: c.problem, ctx: context.Background(), triggers: make(chan trigger, 4), emit: o.emit}
			a.handle(protocol.Message{Type: msgTapAct, Contents: goodTap})
			waitFor(t, "message 111", func() bool { return len(o.of(msgTapResult)) == 1 })
			res := decodeTapResult(t, o.of(msgTapResult)[0])
			if res.OK || res.Message != c.want || res.Refresh {
				t.Fatalf("111 = %+v", res)
			}
			if _, got := queued(a); got {
				t.Fatal("a refresh was queued")
			}
		})
	}
	if n := len(stub.requests()); n != 0 {
		t.Fatalf("the gateway saw %d requests", n)
	}
}

func TestASecondTapWhileOneIsInFlightIsBusyAndNotSent(t *testing.T) {
	stub := newGatewayStub(t, 200, `{"ok":true,"message":"Ticked","refresh":true}`)
	stub.entered, stub.release = make(chan struct{}, 4), make(chan struct{})
	a, o := tapApp(t, stub)

	first := make(chan string, 1)
	go func() {
		reply, _ := a.tapAct(context.Background(), goodTap)
		first <- reply
	}()
	<-stub.entered // the first action is now at the gateway

	reply, refresh := a.tapAct(context.Background(), goodTap)
	res := decodeTapResult(t, reply)
	if res.OK || res.Outcome != "busy" || res.Message == "" || res.Refresh || refresh {
		t.Fatalf("busy reply = %+v (refresh %v)", res, refresh)
	}
	// Through the message path too: the answer is on 111 and nothing is queued.
	a.handle(protocol.Message{Type: msgTapAct, Contents: goodTap})
	waitFor(t, "the busy answer", func() bool { return len(o.of(msgTapResult)) == 1 })
	if r := decodeTapResult(t, o.of(msgTapResult)[0]); r.Outcome != "busy" {
		t.Fatalf("111 = %+v", r)
	}
	if _, got := queued(a); got {
		t.Fatal("a refresh was queued for a tap that was never sent")
	}
	if n := len(stub.requests()); n != 1 {
		t.Fatalf("the gateway saw %d requests while one was in flight", n)
	}

	close(stub.release)
	if res := decodeTapResult(t, <-first); !res.OK {
		t.Fatalf("the first action = %+v", res)
	}

	// The guard is released: the next tap goes through.
	if res := decodeTapResult(t, mustReply(a.tapAct(context.Background(), goodTap))); !res.OK {
		t.Fatalf("a tap after the first finished = %+v", res)
	}
	if n := len(stub.requests()); n != 2 {
		t.Fatalf("the gateway saw %d requests, want 2", n)
	}
}

func mustReply(reply string, _ bool) string { return reply }
