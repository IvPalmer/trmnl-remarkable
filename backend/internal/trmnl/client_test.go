package trmnl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"trmnl-remarkable/backend/internal/config"
)

func TestDisplayHeadersAndFlexibleRate(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("access-token") != "sekret" || r.Header.Get("ID") != "AA:BB" {
			t.Errorf("missing auth headers")
		}
		if r.Header.Get("battery-voltage") != "3.925" || r.Header.Get("firmware-version") != "2.0.0" {
			t.Errorf("incorrect device metadata headers: %#v", r.Header)
		}
		fmt.Fprint(w, `{"status":0,"image_url":"https://example.test/a.png","refresh_rate":"1800","special_function":"sleep"}`)
	}))
	defer s.Close()
	c := New()
	c.Version = "2.0.0"
	c.Battery = func() string { return "3.925" }
	cfg := config.Defaults()
	cfg.BaseURL = s.URL
	cfg.APIKey = "sekret"
	cfg.DeviceID = "AA:BB"
	r, err := c.Display(context.Background(), cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if int(r.RefreshRate) != 1800 || r.SpecialFunction != "sleep" {
		t.Fatalf("unexpected response: %#v", r)
	}
}

func TestDownloadUsesConditionalValidators(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `"screen-v1"` || r.Header.Get("If-Modified-Since") == "" {
			t.Errorf("conditional validators missing: %#v", r.Header)
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer s.Close()
	c := New()
	cfg := config.Defaults()
	cfg.BaseURL = s.URL
	_, _, err := c.Download(context.Background(), cfg, s.URL+"/screen.png", time.Second, `"screen-v1"`, time.Now().Add(-time.Hour).Format(http.TimeFormat))
	if !errors.Is(err, ErrNotModified) {
		t.Fatalf("expected ErrNotModified, got %v", err)
	}
}

func TestCurrentEndpointFallback(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/display/current" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"image_url":"https://example.test/a.bmp","refresh_rate":60}`)
	}))
	defer s.Close()
	c := New()
	cfg := config.Defaults()
	cfg.BaseURL = s.URL
	if _, err := c.Display(context.Background(), cfg, false); err != nil {
		t.Fatal(err)
	}
}

func TestClientRefusesCrossOriginRedirectWithoutLeakingToken(t *testing.T) {
	leaked := false
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("access-token") != ""
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer source.Close()

	client := New()
	client.HTTP.Transport = source.Client().Transport
	cfg := config.Defaults()
	cfg.BaseURL = source.URL
	cfg.APIKey = "secret-token"
	_, err := client.Display(context.Background(), cfg, true)
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("expected redirect refusal, got %v", err)
	}
	if leaked {
		t.Fatal("access token leaked to redirected origin")
	}
}

func TestDownloadRejectsRemoteHTTP(t *testing.T) {
	client := New()
	_, _, err := client.Download(context.Background(), config.Defaults(), "http://192.0.2.1/image.png", time.Second, "", "")
	if err == nil {
		t.Fatal("remote HTTP image URL was accepted")
	}
}

func TestDownloadAllowsHTTPSCDNRedirectWithoutCredentials(t *testing.T) {
	leaked := false
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("access-token") != ""
		fmt.Fprint(w, "image")
	}))
	defer destination.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/image.png", http.StatusFound)
	}))
	defer source.Close()
	client := New()
	client.HTTP.Transport = source.Client().Transport
	cfg := config.Defaults()
	cfg.BaseURL = source.URL
	cfg.APIKey = "secret-token"
	body, _, err := client.Download(context.Background(), cfg, source.URL+"/redirect", time.Second, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	if leaked {
		t.Fatal("image redirect received an API credential")
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	if got := parseRetryAfter("300", now); got != 5*time.Minute {
		t.Fatalf("seconds retry-after = %v", got)
	}
	if got := parseRetryAfter(now.Add(10*time.Minute).Format(http.TimeFormat), now); got != 10*time.Minute {
		t.Fatalf("date retry-after = %v", got)
	}
}

func download(t *testing.T, cfg config.Config, imageURL string) (string, error) {
	t.Helper()
	body, _, err := New().Download(context.Background(), cfg, imageURL, time.Second, "", "")
	if err != nil {
		return "", err
	}
	defer body.Close()
	b, _ := io.ReadAll(body)
	return string(b), nil
}

func TestDownloadSendsTokenOnTheDashboardOriginOnly(t *testing.T) {
	var sameSeen, otherSeen string
	same := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sameSeen = r.Header.Get("access-token")
		fmt.Fprint(w, "image")
	}))
	defer same.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherSeen = r.Header.Get("access-token")
		fmt.Fprint(w, "image")
	}))
	defer other.Close()
	cfg := config.Defaults()
	cfg.BaseURL = same.URL
	cfg.APIKey = "secret-token"
	if _, err := download(t, cfg, same.URL+"/a.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := download(t, cfg, other.URL+"/a.png"); err != nil {
		t.Fatal(err)
	}
	if sameSeen != "secret-token" {
		t.Fatalf("same-origin image got token %q", sameSeen)
	}
	if otherSeen != "" {
		t.Fatalf("another origin (same host, other port) got the token %q", otherSeen)
	}
	// Without a key there is no header to send.
	cfg.APIKey = ""
	if _, err := download(t, cfg, same.URL+"/a.png"); err != nil || sameSeen != "" {
		t.Fatalf("empty key sent a header %q, err %v", sameSeen, err)
	}
}

func TestDownloadRedirectKeepsTokenOnOriginAndDropsItAfterLeaving(t *testing.T) {
	finalSeen, midSeen := "unset", "unset"
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		midSeen = r.Header.Get("access-token")
		// Back to the dashboard's origin: allowed, and the key may travel again.
		http.Redirect(w, r, r.URL.Query().Get("back"), http.StatusFound)
	}))
	defer other.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/away":
			http.Redirect(w, r, other.URL+"/hop?back="+url.QueryEscape("http://"+r.Host+"/final"), http.StatusFound)
		case "/final":
			finalSeen = r.Header.Get("access-token")
			fmt.Fprint(w, "image")
		}
	}))
	defer source.Close()
	cfg := config.Defaults()
	cfg.BaseURL = source.URL
	cfg.APIKey = "secret-token"

	if _, err := download(t, cfg, source.URL+"/same"); err != nil || finalSeen != "secret-token" {
		t.Fatalf("same-origin redirect: token %q, err %v", finalSeen, err)
	}
	finalSeen = "unset"
	if _, err := download(t, cfg, source.URL+"/away"); err != nil {
		t.Fatal(err)
	}
	if midSeen != "" {
		t.Fatalf("the token followed a redirect to another origin: %q", midSeen)
	}
	if finalSeen != "secret-token" {
		t.Fatalf("a redirect back to the dashboard origin lost the token: %q", finalSeen)
	}
}

func TestDownloadRefusesUnsafeRedirectTargets(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://192.0.2.1/x.png", http.StatusFound)
	}))
	defer source.Close()
	cfg := config.Defaults()
	cfg.BaseURL = source.URL
	if _, err := download(t, cfg, source.URL+"/a.png"); err == nil || !strings.Contains(err.Error(), "unsafe image redirect") {
		t.Fatalf("redirect to plain HTTP elsewhere: %v", err)
	}
}

// proxyStub stands in for the loopback forwarder: an HTTP proxy receives the
// request in absolute form, so r.Host and r.URL name the real destination.
func proxyStub(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) config.Config {
	t.Helper()
	p := httptest.NewServer(http.HandlerFunc(handle))
	t.Cleanup(p.Close)
	cfg := config.Defaults()
	cfg.Proxy = p.URL
	cfg.BaseURL = "http://dash.example.ts.net:3000"
	cfg.APIKey = "secret-token"
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDisplayAndDownloadGoThroughTheProxy(t *testing.T) {
	var hosts []string
	cfg := proxyStub(t, func(w http.ResponseWriter, r *http.Request) {
		hosts = append(hosts, r.Host+r.URL.Path+"|"+r.Header.Get("access-token"))
		if r.URL.Path == "/api/display" {
			fmt.Fprint(w, `{"image_url":"http://dash.example.ts.net:3000/byos/images/a.png","refresh_rate":60}`)
			return
		}
		fmt.Fprint(w, "image")
	})
	r, err := New().Display(context.Background(), cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	body, err := download(t, cfg, r.ImageURL)
	if err != nil || body != "image" {
		t.Fatalf("download through the proxy: %q, %v", body, err)
	}
	want := []string{
		"dash.example.ts.net:3000/api/display|secret-token",
		"dash.example.ts.net:3000/byos/images/a.png|secret-token",
	}
	if strings.Join(hosts, ",") != strings.Join(want, ",") {
		t.Fatalf("proxy saw %v, want %v", hosts, want)
	}
}

func TestTailnetHTTPIsRefusedWithoutAProxy(t *testing.T) {
	cfg := config.Defaults()
	if _, err := download(t, cfg, "http://dash.example.ts.net:3000/a.png"); err == nil {
		t.Fatal("a plain-HTTP tailnet image was fetched with no proxy configured")
	}
	// A proxy that is configured per call applies per call: the same client
	// used again without one is back to the strict rule.
	c := New()
	var viaProxy int
	proxied := proxyStub(t, func(w http.ResponseWriter, r *http.Request) { viaProxy++; fmt.Fprint(w, "image") })
	if body, _, err := c.Download(context.Background(), proxied, "http://dash.example.ts.net:3000/a.png", time.Second, "", ""); err != nil {
		t.Fatal(err)
	} else {
		body.Close()
	}
	if _, _, err := c.Download(context.Background(), cfg, "http://dash.example.ts.net:3000/a.png", time.Second, "", ""); err == nil {
		t.Fatal("the proxy setting leaked into a later call without one")
	}
	if viaProxy != 1 {
		t.Fatalf("proxy served %d requests, want 1", viaProxy)
	}
}

func tapJSON(extra string) string {
	return `{"x":10,"y":20,"w":300,"h":200,"widget":"vault.money","key":"tmpl:abc","title":"Rent",` +
		`"actions":[{"id":"confirm_payment","label":"Mark paid","confirm":"Mark {title} paid?"}]` + extra + `}`
}

func TestParseTapsAcceptsAValidList(t *testing.T) {
	taps, screen, err := parseTaps(json.RawMessage(`[`+tapJSON("")+`,{"x":0,"y":0,"w":10000,"h":1,"widget":"a.b","key":"k","title":"","actions":[]}]`), json.RawMessage(`"page1"`))
	if err != nil || screen != "page1" || len(taps) != 2 {
		t.Fatalf("taps=%v screen=%q err=%v", taps, screen, err)
	}
	want := Tap{X: 10, Y: 20, W: 300, H: 200, Widget: "vault.money", Key: "tmpl:abc", Title: "Rent",
		Actions: []TapAction{{ID: "confirm_payment", Label: "Mark paid", Confirm: "Mark {title} paid?"}}}
	if fmt.Sprint(taps[0]) != fmt.Sprint(want) {
		t.Fatalf("tap = %#v, want %#v", taps[0], want)
	}
	if taps[1].Actions == nil {
		t.Fatal("a tap with no actions must carry an empty list, not nil")
	}
	for _, none := range []string{``, `null`, `[]`} {
		if taps, screen, err := parseTaps(json.RawMessage(none), nil); taps != nil || screen != "" || err != nil {
			t.Fatalf("%q: taps=%v screen=%q err=%v", none, taps, screen, err)
		}
	}
}

func TestParseTapsDropsEverythingWhenOneThingIsBad(t *testing.T) {
	long := strings.Repeat("é", 501)
	hundredOne := "[" + strings.TrimSuffix(strings.Repeat(tapJSON("")+",", 101), ",") + "]"
	cases := map[string]struct{ taps, screen string }{
		"101 taps":                 {hundredOne, `"page1"`},
		"negative x":               {`[` + strings.Replace(tapJSON(""), `"x":10`, `"x":-1`, 1) + `]`, `"page1"`},
		"x over range":             {`[` + strings.Replace(tapJSON(""), `"x":10`, `"x":10001`, 1) + `]`, `"page1"`},
		"fractional y":             {`[` + strings.Replace(tapJSON(""), `"y":20`, `"y":20.5`, 1) + `]`, `"page1"`},
		"string x":                 {`[` + strings.Replace(tapJSON(""), `"x":10`, `"x":"10"`, 1) + `]`, `"page1"`},
		"zero width":               {`[` + strings.Replace(tapJSON(""), `"w":300`, `"w":0`, 1) + `]`, `"page1"`},
		"zero height":              {`[` + strings.Replace(tapJSON(""), `"h":200`, `"h":0`, 1) + `]`, `"page1"`},
		"missing y":                {`[` + strings.Replace(tapJSON(""), `"y":20,`, ``, 1) + `]`, `"page1"`},
		"widget without a dot":     {`[` + strings.Replace(tapJSON(""), `vault.money`, `vault`, 1) + `]`, `"page1"`},
		"widget upper case":        {`[` + strings.Replace(tapJSON(""), `vault.money`, `Vault.money`, 1) + `]`, `"page1"`},
		"widget app too long":      {`[` + strings.Replace(tapJSON(""), `vault.money`, `abcdefghijklmnopq.money`, 1) + `]`, `"page1"`},
		"empty key":                {`[` + strings.Replace(tapJSON(""), `tmpl:abc`, ``, 1) + `]`, `"page1"`},
		"long key":                 {`[` + strings.Replace(tapJSON(""), `tmpl:abc`, long, 1) + `]`, `"page1"`},
		"long title":               {`[` + strings.Replace(tapJSON(""), `Rent`, long, 1) + `]`, `"page1"`},
		"long label":               {`[` + strings.Replace(tapJSON(""), `Mark paid`, long, 1) + `]`, `"page1"`},
		"long confirm":             {`[` + strings.Replace(tapJSON(""), `Mark {title} paid?`, long, 1) + `]`, `"page1"`},
		"empty label":              {`[` + strings.Replace(tapJSON(""), `Mark paid`, ``, 1) + `]`, `"page1"`},
		"action id upper case":     {`[` + strings.Replace(tapJSON(""), `confirm_payment`, `Confirm`, 1) + `]`, `"page1"`},
		"action id starts a digit": {`[` + strings.Replace(tapJSON(""), `confirm_payment`, `1pay`, 1) + `]`, `"page1"`},
		"nine actions":             {`[` + strings.Replace(tapJSON(""), `"actions":[`, `"actions":[`+strings.Repeat(`{"id":"a","label":"A"},`, 8), 1) + `]`, `"page1"`},
		"not a list":               {`{"x":1}`, `"page1"`},
		"not objects":              {`[1,2]`, `"page1"`},
		"screen missing":           {`[` + tapJSON("") + `]`, ``},
		"screen null":              {`[` + tapJSON("") + `]`, `null`},
		"screen upper":             {`[` + tapJSON("") + `]`, `"Page1"`},
		"screen digit":             {`[` + tapJSON("") + `]`, `"1page"`},
		"screen too long":          {`[` + tapJSON("") + `]`, `"abcdefghijklmnopq"`},
		"screen not text":          {`[` + tapJSON("") + `]`, `5`},
		"second tap is bad":        {`[` + tapJSON("") + `,` + strings.Replace(tapJSON(""), `"w":300`, `"w":-3`, 1) + `]`, `"page1"`},
	}
	for name, c := range cases {
		taps, screen, err := parseTaps(json.RawMessage(c.taps), json.RawMessage(c.screen))
		if err == nil || taps != nil || screen != "" {
			t.Errorf("%s: taps=%v screen=%q err=%v; want an error and nothing kept", name, taps, screen, err)
		}
	}
	// The edge values stay valid.
	edge := `[` + strings.Replace(strings.Replace(tapJSON(""), `Rent`, strings.Repeat("é", 500), 1), `"x":10`, `"x":10000`, 1) + `]`
	if _, _, err := parseTaps(json.RawMessage(edge), json.RawMessage(`"abcdefghijklmnop"`)); err != nil {
		t.Errorf("boundary values refused: %v", err)
	}
	hundred := "[" + strings.TrimSuffix(strings.Repeat(tapJSON("")+",", 100), ",") + "]"
	if taps, _, err := parseTaps(json.RawMessage(hundred), json.RawMessage(`"page1"`)); err != nil || len(taps) != 100 {
		t.Errorf("100 taps: %d, %v", len(taps), err)
	}
}

func TestDisplayKeepsTheImageWhenTapsAreBad(t *testing.T) {
	var body string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	defer s.Close()
	cfg := config.Defaults()
	cfg.BaseURL = s.URL
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(io.Discard)

	body = `{"image_url":"` + s.URL + `/a.png","refresh_rate":60,"tap_screen":"page1","taps":[` + tapJSON("") + `]}`
	r, err := New().Display(context.Background(), cfg, true)
	if err != nil || len(r.Taps) != 1 || r.TapScreen != "page1" || r.Taps[0].Widget != "vault.money" {
		t.Fatalf("valid taps: %#v, %v", r, err)
	}
	if logs.Len() != 0 {
		t.Fatalf("a valid response logged %q", logs.String())
	}

	body = `{"image_url":"` + s.URL + `/a.png","refresh_rate":60,"tap_screen":"page1","taps":[` + tapJSON("") + `,{"x":"bad"}]}`
	r, err = New().Display(context.Background(), cfg, true)
	if err != nil || r.ImageURL == "" || len(r.Taps) != 0 || r.TapScreen != "" {
		t.Fatalf("bad taps must drop only the taps: %#v, %v", r, err)
	}
	if n := strings.Count(logs.String(), "taps ignored"); n != 1 {
		t.Fatalf("logged %d times: %q", n, logs.String())
	}

	// A server that knows nothing about taps is unaffected.
	body = `{"image_url":"` + s.URL + `/a.png","refresh_rate":60}`
	logs.Reset()
	if r, err = New().Display(context.Background(), cfg, true); err != nil || r.Taps != nil || logs.Len() != 0 {
		t.Fatalf("plain response: %#v, %v, %q", r, err, logs.String())
	}
}
