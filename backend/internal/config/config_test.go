package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSaveLoadAndPermissions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "config.json")
	c := Defaults()
	c.APIKey = "secret"
	c.BrightnessPercent = 150
	c.UseSystemBrightness = false
	c.BrightnessScheduleEnabled = true
	c.BrightnessSchedule = []int{25}
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "secret" || got.BrightnessPercent != 100 || !got.BrightnessScheduleEnabled || len(got.BrightnessSchedule) != BrightnessScheduleSize || got.BrightnessSchedule[0] != 25 {
		t.Fatalf("unexpected config: %#v", got)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0600 {
			t.Fatalf("mode=%o", st.Mode().Perm())
		}
	}
}

func TestDefaultsEnableScheduledWake(t *testing.T) {
	if !Defaults().WakeForRefresh {
		t.Fatal("scheduled wake should be enabled by default")
	}
}

func TestBrightnessScheduleStartsOnMondayAndUsesHalfHours(t *testing.T) {
	c := Defaults()
	c.BrightnessScheduleEnabled = true
	c.BrightnessSchedule = make([]int, BrightnessScheduleSize)
	for i := range c.BrightnessSchedule {
		c.BrightnessSchedule[i] = -1
	}
	c.BrightnessSchedule[17*7] = 35
	c.BrightnessSchedule[47*7+6] = 5

	monday := time.Date(2026, 8, 3, 8, 42, 0, 0, time.Local)
	if got, ok := c.ScheduledBrightness(monday); !ok || got != 35 {
		t.Fatalf("Monday 08:42 = %d, %v; want 35, true", got, ok)
	}
	sunday := time.Date(2026, 8, 9, 23, 59, 0, 0, time.Local)
	if got, ok := c.ScheduledBrightness(sunday); !ok || got != 5 {
		t.Fatalf("Sunday 23:59 = %d, %v; want 5, true", got, ok)
	}
	if _, ok := c.ScheduledBrightness(monday.Add(30 * time.Minute)); ok {
		t.Fatal("an unassigned slot should use the default brightness")
	}
}

func TestNormalizeBrightnessSchedule(t *testing.T) {
	c := Defaults()
	c.BrightnessScheduleEnabled = true
	c.BrightnessSchedule = []int{-9, 150, 40}
	c.UseSystemBrightness = false
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	if len(c.BrightnessSchedule) != BrightnessScheduleSize {
		t.Fatalf("schedule length = %d", len(c.BrightnessSchedule))
	}
	if c.BrightnessSchedule[0] != -1 || c.BrightnessSchedule[1] != 100 || c.BrightnessSchedule[2] != 40 || c.BrightnessSchedule[3] != -1 {
		t.Fatalf("schedule was not normalized: %v", c.BrightnessSchedule[:4])
	}

	c = Defaults()
	c.BrightnessScheduleEnabled = true
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	if c.BrightnessScheduleEnabled {
		t.Fatal("an empty schedule should not remain enabled")
	}

	c.BrightnessSchedule = make([]int, BrightnessScheduleSize)
	c.BrightnessScheduleEnabled = true
	c.UseSystemBrightness = true
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	if c.BrightnessScheduleEnabled {
		t.Fatal("system brightness and the custom schedule cannot both be enabled")
	}
}

func TestRejectsBadURL(t *testing.T) {
	c := Defaults()
	c.BaseURL = "example.test"
	if err := c.Normalize(); err == nil {
		t.Fatal("expected invalid URL")
	}
}

func TestRejectsInsecureRemoteURL(t *testing.T) {
	c := Defaults()
	c.BaseURL = "http://192.168.1.20:3000"
	if err := c.Normalize(); err == nil {
		t.Fatal("expected remote HTTP URL to be rejected")
	}
	c.BaseURL = "http://127.0.0.1:9988"
	if err := c.Normalize(); err != nil {
		t.Fatalf("loopback mock URL should remain available: %v", err)
	}
}

func TestRejectsUnsafeServerURLFeatures(t *testing.T) {
	for _, raw := range []string{
		"https://user:secret@example.test",
		"https://example.test/trmnl",
		"https://example.test?token=secret",
		"https://example.test/#fragment",
	} {
		c := Defaults()
		c.BaseURL = raw
		if err := c.Normalize(); err == nil {
			t.Fatalf("Normalize accepted unsafe server URL %q", raw)
		}
	}
}

func TestValidateDashboardURL(t *testing.T) {
	tests := []struct {
		url              string
		plain, withProxy bool // accepted without a proxy / with one
	}{
		{"https://cdn.example.test/screens/a.png?token=opaque", true, true},
		{"https://x.ts.net", true, true},
		{"http://127.0.0.1:9988/image/test.png", true, true},
		{"http://[::1]:9988/image/test.png", true, true},
		{"http://localhost:3000", true, true},
		{"http://x.ts.net", false, true},
		{"http://X.Example.TS.NET:3000/api", false, true},
		{"http://100.100.1.1", false, true},
		{"http://100.64.0.1:8080", false, true},
		{"http://100.127.255.254", false, true},
		{"http://100.63.255.255", false, false},
		{"http://100.63.0.1", false, false},
		{"http://100.128.0.1", false, false},
		{"http://example.com", false, false},
		{"http://ts.net", false, false},
		{"http://evil-ts.net", false, false},
		{"http://x.ts.net.example.com", false, false},
		{"http://192.0.2.20/image.png", false, false},
		{"http://198.51.100.5", false, false},
		{"ftp://x.ts.net/image.png", false, false},
		{"//example.test/image.png", false, false},
		{"http://user:pass@x.ts.net", false, false},
		{"https://user:pass@example.test", false, false},
		{"", false, false},
	}
	for _, tc := range tests {
		for _, proxied := range []bool{false, true} {
			want := tc.plain
			if proxied {
				want = tc.withProxy
			}
			if err := ValidateDashboardURL(tc.url, proxied); (err == nil) != want {
				t.Errorf("ValidateDashboardURL(%q, proxied=%v) = %v; accepted should be %v", tc.url, proxied, err, want)
			}
		}
	}
}

func TestNormalizeProxy(t *testing.T) {
	for _, proxy := range []string{"http://127.0.0.1:8080", " http://localhost:3128/ ", "http://[::1]:9000"} {
		c := Defaults()
		c.Proxy = proxy
		c.BaseURL = "http://x.ts.net"
		if err := c.Normalize(); err != nil {
			t.Errorf("proxy %q refused: %v", proxy, err)
		}
		if c.Proxy == "" || c.Proxy[len(c.Proxy)-1] == '/' || c.Proxy[0] == ' ' {
			t.Errorf("proxy %q was not trimmed: %q", proxy, c.Proxy)
		}
	}
	for _, proxy := range []string{
		"https://127.0.0.1:8080", "http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:70000",
		"http://192.0.2.1:8080", "http://x.ts.net:8080", "http://127.0.0.1:8080/path",
		"http://127.0.0.1:8080?x=1", "http://user:pw@127.0.0.1:8080", "socks5://127.0.0.1:1080", "127.0.0.1:8080",
	} {
		c := Defaults()
		c.Proxy = proxy
		if err := c.Normalize(); err == nil {
			t.Errorf("proxy %q was accepted", proxy)
		}
	}
}

func TestTailnetBaseURLNeedsTheProxy(t *testing.T) {
	c := Defaults()
	c.BaseURL = "http://x.ts.net:3000"
	if err := c.Normalize(); err == nil {
		t.Fatal("a plain-HTTP tailnet URL was accepted without a proxy")
	}
	c.Proxy = "http://127.0.0.1:8080"
	if err := c.Normalize(); err != nil {
		t.Fatalf("a tailnet URL behind a loopback proxy was refused: %v", err)
	}
	// The proxy survives a save and load.
	p := filepath.Join(t.TempDir(), "config.json")
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || got.Proxy != "http://127.0.0.1:8080" {
		t.Fatalf("proxy not round-tripped: %q, %v", got.Proxy, err)
	}
}

func TestQuietHoursWrapsPastMidnight(t *testing.T) {
	c := Defaults()
	c.QuietHoursEnabled = true
	c.QuietHoursStart, c.QuietHoursEnd = "23:00", "07:00"
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 8, 5, hour, minute, 0, 0, time.UTC)
	}
	for _, quiet := range []time.Time{at(23, 0), at(23, 30), at(0, 15), at(6, 59)} {
		if !c.InQuietHours(quiet) {
			t.Fatalf("%s should be inside the quiet window", quiet.Format("15:04"))
		}
	}
	for _, active := range []time.Time{at(7, 0), at(12, 0), at(22, 59)} {
		if c.InQuietHours(active) {
			t.Fatalf("%s should be outside the quiet window", active.Format("15:04"))
		}
	}
	// A refresh landing at 01:00 is deferred to the 07:00 resume on the same day.
	if got := c.NextActiveTime(at(1, 0)); !got.Equal(at(7, 0)) {
		t.Fatalf("NextActiveTime(01:00) = %s, want 07:00", got.Format("15:04"))
	}
	// A refresh landing at 23:30 is deferred to 07:00 the following morning.
	want := at(7, 0).AddDate(0, 0, 1)
	if got := c.NextActiveTime(at(23, 30)); !got.Equal(want) {
		t.Fatalf("NextActiveTime(23:30) = %s, want %s", got, want)
	}
	// Times outside the window are untouched.
	if got := c.NextActiveTime(at(12, 0)); !got.Equal(at(12, 0)) {
		t.Fatalf("NextActiveTime(12:00) = %s, want it unchanged", got.Format("15:04"))
	}
}

func TestQuietHoursDisabledAndDaytimeWindow(t *testing.T) {
	c := Defaults()
	at := time.Date(2026, 8, 5, 23, 30, 0, 0, time.UTC)
	if c.InQuietHours(at) {
		t.Fatal("quiet hours applied while disabled")
	}
	c.QuietHoursEnabled = true
	c.QuietHoursStart, c.QuietHoursEnd = "09:00", "17:00"
	if !c.InQuietHours(time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("a same-day window should contain noon")
	}
	if c.InQuietHours(at) {
		t.Fatal("a same-day window should not contain 23:30")
	}
}

func TestNormalizeValidatesNewSettings(t *testing.T) {
	c := Defaults()
	c.QuietHoursStart = "25:00"
	if err := c.Normalize(); err == nil {
		t.Fatal("an invalid quiet-hours time was accepted")
	}
	c = Defaults()
	c.DitherPalette = []string{"#00ff00", "not-a-colour"}
	if err := c.Normalize(); err == nil {
		t.Fatal("an invalid palette colour was accepted")
	}
	c = Defaults()
	c.Dither = "nonsense"
	c.BatterySaverPercent = 99
	if err := c.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if c.Dither != "off" || c.BatterySaverPercent != 0 {
		t.Fatalf("out-of-range values were not clamped: dither=%q saver=%d", c.Dither, c.BatterySaverPercent)
	}
	if len(c.Palette()) == 0 {
		t.Fatal("Palette() returned nothing for a default config")
	}
}
