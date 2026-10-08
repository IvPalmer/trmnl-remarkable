package trmnl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"trmnl-remarkable/backend/internal/config"
)

type FlexibleInt int

func (f *FlexibleInt) UnmarshalJSON(b []byte) error {
	var n int
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		v, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		n = v
	} else if string(b) != "null" {
		if err := json.Unmarshal(b, &n); err != nil {
			return err
		}
	}
	*f = FlexibleInt(n)
	return nil
}

type DisplayResponse struct {
	Status               int         `json:"status"`
	ImageURL             string      `json:"image_url"`
	ImageName            string      `json:"image_name"`
	Filename             string      `json:"filename"`
	RefreshRate          FlexibleInt `json:"refresh_rate"`
	ImageURLTimeout      FlexibleInt `json:"image_url_timeout"`
	SpecialFunction      string      `json:"special_function"`
	UpdateFirmware       bool        `json:"update_firmware"`
	ResetFirmware        bool        `json:"reset_firmware"`
	MaximumCompatibility bool        `json:"maximum_compatibility"`

	// Taps and TapScreen are the BYOS extension described in docs/BYOS.md.
	// They are filled by get after validation, never by the JSON decoder, so a
	// bad tap list can cost the taps but never the image.
	Taps      []Tap  `json:"-"`
	TapScreen string `json:"-"`
}

// Tap is a region of the image a BYOS server marks as actionable, in pixels of
// the image it served. Actions are what the server's gateway offers for it.
type Tap struct {
	X       int         `json:"x"`
	Y       int         `json:"y"`
	W       int         `json:"w"`
	H       int         `json:"h"`
	Widget  string      `json:"widget"`
	Key     string      `json:"key"`
	Title   string      `json:"title"`
	Actions []TapAction `json:"actions"`
}

type TapAction struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Confirm string `json:"confirm,omitempty"`
}

// Limits for a tap list. A list over any of them is dropped whole.
const (
	MaxTaps       = 100
	MaxTapActions = 8
	MaxTapString  = 500
	MaxTapCoord   = 10000
)

var (
	// WidgetIDPattern, ScreenPattern and ActionIDPattern are the gateway's
	// own id formats (its widget contract), so nothing else is ever sent back.
	WidgetIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]{0,15}\.[a-z][a-z0-9_]{0,31}$`)
	ScreenPattern   = regexp.MustCompile(`^[a-z][a-z0-9]{0,15}$`)
	ActionIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

// rawTap decodes numbers as float64 so that "5" (a string) and 10.5 are
// distinguishable from the integers the contract asks for; a missing field is
// nil.
type rawTap struct {
	X, Y, W, H *float64
	Widget     string
	Key        string
	Title      string
	Actions    []TapAction
}

func tapCoord(v *float64, name string, min int) (int, error) {
	if v == nil || *v != math.Trunc(*v) || *v < float64(min) || *v > MaxTapCoord {
		return 0, fmt.Errorf("%s must be an integer from %d to %d", name, min, MaxTapCoord)
	}
	return int(*v), nil
}

func tapString(s, name string) error {
	if len([]rune(s)) > MaxTapString {
		return fmt.Errorf("%s is longer than %d characters", name, MaxTapString)
	}
	return nil
}

// parseTaps validates the taps and tap_screen of a display response. An absent
// or empty list is not an error and yields nothing. Anything else wrong drops
// the whole list: a half-trusted overlay could send an action for the wrong
// region.
func parseTaps(rawTaps, rawScreen json.RawMessage) ([]Tap, string, error) {
	if len(bytes.TrimSpace(rawTaps)) == 0 || string(bytes.TrimSpace(rawTaps)) == "null" {
		return nil, "", nil
	}
	var in []rawTap
	if err := json.Unmarshal(rawTaps, &in); err != nil {
		return nil, "", fmt.Errorf("taps are not a list of tap objects: %w", err)
	}
	if len(in) == 0 {
		return nil, "", nil
	}
	if len(in) > MaxTaps {
		return nil, "", fmt.Errorf("%d taps, at most %d allowed", len(in), MaxTaps)
	}
	var screen string
	if err := json.Unmarshal(rawScreen, &screen); err != nil || !ScreenPattern.MatchString(screen) {
		return nil, "", errors.New("tap_screen is missing or invalid")
	}
	out := make([]Tap, 0, len(in))
	for i, r := range in {
		bad := func(err error) ([]Tap, string, error) { return nil, "", fmt.Errorf("tap %d: %w", i, err) }
		var t Tap
		var err error
		if t.X, err = tapCoord(r.X, "x", 0); err != nil {
			return bad(err)
		}
		if t.Y, err = tapCoord(r.Y, "y", 0); err != nil {
			return bad(err)
		}
		if t.W, err = tapCoord(r.W, "w", 1); err != nil {
			return bad(err)
		}
		if t.H, err = tapCoord(r.H, "h", 1); err != nil {
			return bad(err)
		}
		if !WidgetIDPattern.MatchString(r.Widget) {
			return bad(errors.New("widget is not an app.id"))
		}
		if r.Key == "" {
			return bad(errors.New("key is empty"))
		}
		if err := errors.Join(tapString(r.Key, "key"), tapString(r.Title, "title")); err != nil {
			return bad(err)
		}
		if len(r.Actions) > MaxTapActions {
			return bad(fmt.Errorf("%d actions, at most %d allowed", len(r.Actions), MaxTapActions))
		}
		for _, a := range r.Actions {
			if !ActionIDPattern.MatchString(a.ID) {
				return bad(errors.New("action id is invalid"))
			}
			if a.Label == "" {
				return bad(errors.New("action label is empty"))
			}
			if err := errors.Join(tapString(a.Label, "action label"), tapString(a.Confirm, "action confirm")); err != nil {
				return bad(err)
			}
		}
		t.Widget, t.Key, t.Title, t.Actions = r.Widget, r.Key, r.Title, r.Actions
		if t.Actions == nil {
			t.Actions = []TapAction{}
		}
		out = append(out, t)
	}
	return out, screen, nil
}

type Client struct {
	HTTP    *http.Client
	Version string
	// Model is sent as the "model" header so a BYOS server can tell the
	// tablets apart. It defaults to the Paper Pro, which is what every
	// installation reported before the reMarkable 1 and 2 were supported.
	Model   string
	Battery func() string
	RSSI    func() string

	mu      sync.Mutex
	proxied *proxiedTransport
}

// proxiedTransport is the transport used while a proxy is configured. It is
// kept between calls so keep-alive connections are reused, and rebuilt when
// the proxy setting changes.
type proxiedTransport struct {
	proxy string
	rt    http.RoundTripper
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: secureRedirect}, Version: "dev"}
}

func secureRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	initial := via[0].URL
	if !strings.EqualFold(initial.Scheme, req.URL.Scheme) || !strings.EqualFold(initial.Host, req.URL.Host) {
		return errors.New("cross-origin or protocol-changing redirect refused")
	}
	return nil
}

// originOf is scheme://host:port with the default port made explicit, so
// "https://x" and "https://x:443" are the same origin.
func originOf(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	return scheme + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

// SameOrigin reports whether two URLs share scheme, host and port.
func SameOrigin(a, b string) bool {
	ua, errA := url.Parse(strings.TrimSpace(a))
	ub, errB := url.Parse(strings.TrimSpace(b))
	return errA == nil && errB == nil && ua.Hostname() != "" && ub.Hostname() != "" && originOf(ua) == originOf(ub)
}

const tokenHeader = "access-token"

// imageRedirectPolicy checks every hop of an image download against the
// transport rules and takes the credential off any hop that has left the
// dashboard's origin (Go forwards custom headers across redirects).
func imageRedirectPolicy(cfg config.Config) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if err := config.ValidateDashboardURL(req.URL.String(), cfg.Proxy != ""); err != nil {
			return fmt.Errorf("unsafe image redirect: %w", err)
		}
		if !SameOrigin(cfg.BaseURL, req.URL.String()) {
			req.Header.Del(tokenHeader)
		}
		return nil
	}
}

// httpFor returns the client to use for one call under cfg. Without a proxy
// that is the shared client. With one, a copy whose transport goes through it;
// config can change while the app runs, so this is decided per call.
func (c *Client) httpFor(cfg config.Config) *http.Client {
	if cfg.Proxy == "" {
		return c.HTTP
	}
	proxyURL, err := url.Parse(cfg.Proxy)
	if err != nil {
		return c.HTTP
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proxied == nil || c.proxied.proxy != cfg.Proxy {
		var t *http.Transport
		if base, ok := c.HTTP.Transport.(*http.Transport); ok {
			t = base.Clone()
		} else if def, ok := http.DefaultTransport.(*http.Transport); ok {
			t = def.Clone()
		} else {
			t = &http.Transport{}
		}
		t.Proxy = http.ProxyURL(proxyURL)
		c.proxied = &proxiedTransport{proxy: cfg.Proxy, rt: t}
	}
	h := *c.HTTP
	h.Transport = c.proxied.rt
	return &h
}

func (c *Client) Display(ctx context.Context, cfg config.Config, advance bool) (DisplayResponse, error) {
	paths := []string{"/api/display"}
	if !advance {
		// The first two paths are non-advancing. Older BYOS implementations
		// expose neither, so /api/display is the compatibility fallback.
		paths = []string{"/api/display/current", "/api/current_screen", "/api/display"}
	}
	var last error
	for _, p := range paths {
		resp, err := c.get(ctx, cfg, p)
		if err == nil {
			return resp, nil
		}
		last = err
		var he *HTTPError
		if advance || !errors.As(err, &he) || he.Status != http.StatusNotFound {
			break
		}
	}
	return DisplayResponse{}, last
}

type HTTPError struct {
	Status     int
	Body       string
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string { return fmt.Sprintf("TRMNL server returned HTTP %d", e.Status) }

func (c *Client) get(ctx context.Context, cfg config.Config, path string) (DisplayResponse, error) {
	base, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil {
		return DisplayResponse{}, err
	}
	ref, _ := url.Parse(path)
	endpoint := base.ResolveReference(ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return DisplayResponse{}, err
	}
	if cfg.APIKey != "" {
		req.Header.Set("access-token", cfg.APIKey)
	}
	if cfg.DeviceID != "" {
		req.Header.Set("ID", cfg.DeviceID)
	}
	req.Header.Set("User-Agent", "trmnl-remarkable/"+c.Version)
	model := c.Model
	if model == "" {
		model = "reMarkable Paper Pro"
	}
	req.Header.Set("model", model)
	req.Header.Set("firmware-version", c.Version)
	if c.Battery != nil {
		if v := c.Battery(); v != "" {
			req.Header.Set("battery-voltage", v)
		}
	}
	if c.RSSI != nil {
		if v := c.RSSI(); v != "" {
			req.Header.Set("rssi", v)
		}
	}
	r, err := c.httpFor(cfg).Do(req)
	if err != nil {
		return DisplayResponse{}, err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1024))
		return DisplayResponse{}, &HTTPError{Status: r.StatusCode, Body: string(b), RetryAfter: parseRetryAfter(r.Header.Get("Retry-After"), time.Now())}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return DisplayResponse{}, fmt.Errorf("read display response: %w", err)
	}
	var out DisplayResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("decode display response: %w", err)
	}
	var extra struct {
		Taps      json.RawMessage `json:"taps"`
		TapScreen json.RawMessage `json:"tap_screen"`
	}
	if err := json.Unmarshal(body, &extra); err == nil {
		var terr error
		if out.Taps, out.TapScreen, terr = parseTaps(extra.Taps, extra.TapScreen); terr != nil {
			log.Printf("trmnl: taps ignored, image kept: %v", terr)
		}
	}
	if out.ImageURL == "" {
		return out, errors.New("display response contained no image_url")
	}
	if _, err := url.ParseRequestURI(out.ImageURL); err != nil {
		return out, fmt.Errorf("invalid image_url: %w", err)
	}
	return out, nil
}

// Download fetches an image. It goes through cfg.Proxy when one is set, and
// sends the API key only when the image is on the dashboard's own origin
// (scheme, host and port of cfg.BaseURL), including after a redirect.
func (c *Client) Download(ctx context.Context, cfg config.Config, imageURL string, timeout time.Duration, etag, lastModified string) (io.ReadCloser, http.Header, error) {
	if err := config.ValidateDashboardURL(imageURL, cfg.Proxy != ""); err != nil {
		return nil, nil, fmt.Errorf("image URL: %w", err)
	}
	if timeout <= 0 || timeout > 2*time.Minute {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	_ = cancel
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	if cfg.APIKey != "" && SameOrigin(cfg.BaseURL, imageURL) {
		req.Header.Set(tokenHeader, cfg.APIKey)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	downloadClient := *c.httpFor(cfg)
	downloadClient.CheckRedirect = imageRedirectPolicy(cfg)
	r, err := downloadClient.Do(req)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	if r.StatusCode == http.StatusNotModified {
		_ = r.Body.Close()
		cancel()
		return nil, r.Header, ErrNotModified
	}
	if r.StatusCode != http.StatusOK {
		r.Body.Close()
		cancel()
		return nil, nil, fmt.Errorf("image server returned HTTP %d", r.StatusCode)
	}
	return &cancelReadCloser{ReadCloser: r.Body, cancel: cancel}, r.Header, nil
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

var ErrNotModified = errors.New("image not modified")

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelReadCloser) Close() error { err := c.ReadCloser.Close(); c.cancel(); return err }
