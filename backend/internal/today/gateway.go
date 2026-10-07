package today

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is how a Source talks to the gateway. Tests pass a fake.
type Client interface {
	Get(ctx context.Context, path string, out any) error
	Post(ctx context.Context, path string, body, out any) error
}

// ErrTailscaleDown means the local proxy refused the connection: tailscaled
// is not running on the tablet.
var ErrTailscaleDown = errors.New("tailscale is not running on the tablet")

// HTTPError is any answer other than 200. Message is the gateway's
// {"error": …}, if it sent one.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("gateway answered %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("gateway answered %d", e.Status)
}

// Gateway is the one HTTP client every Source shares: the operator's Mac
// gateway, through the tablet's Tailscale proxy, with the Today bearer.
type Gateway struct {
	base  string
	token string
	http  *http.Client
}

const requestTimeout = 15 * time.Second

func NewGateway(cfg Config, token string) (*Gateway, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	tr := &http.Transport{MaxIdleConns: 4, IdleConnTimeout: 90 * time.Second}
	p, err := url.Parse(cfg.Proxy) // validate() requires one: never a direct connection
	if err != nil {
		return nil, err
	}
	tr.Proxy = http.ProxyURL(p)
	return &Gateway{
		base:  strings.TrimRight(cfg.GatewayURL, "/"),
		token: token,
		http: &http.Client{
			Timeout:   requestTimeout,
			Transport: tr,
			// The bearer never follows a redirect anywhere.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (g *Gateway) Get(ctx context.Context, path string, out any) error {
	return g.do(ctx, http.MethodGet, path, nil, out)
}

func (g *Gateway) Post(ctx context.Context, path string, body, out any) error {
	return g.do(ctx, http.MethodPost, path, body, out)
}

func (g *Gateway) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return classify(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		return &HTTPError{Status: resp.StatusCode, Message: e.Error}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("gateway %s %s: unreadable answer: %w", method, path, err)
	}
	return nil
}

// classify marks a refused proxy: net/http wraps every failure to reach the
// proxy in an OpError whose Op is "proxyconnect".
func classify(err error) error {
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "proxyconnect" {
		return fmt.Errorf("%w (%v)", ErrTailscaleDown, op.Err)
	}
	return err
}

// UserMessage is what the operator reads on the tablet for err.
func UserMessage(err error) string {
	var he *HTTPError
	var ne net.Error
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrTailscaleDown):
		return "Tailscale isn't running on the tablet"
	case errors.As(err, &he):
		switch {
		case he.Status == http.StatusUnauthorized:
			return "Tablet not authorised. Run rm-today-setup."
		case he.Status == http.StatusForbidden && he.Message == "peer not allowed":
			return "The Mac doesn't recognise this tablet yet. Try again in a minute."
		case he.Status == http.StatusForbidden:
			return "Tablet not authorised. Run rm-today-setup."
		case he.Message != "":
			return he.Message
		case he.Status >= 502 && he.Status <= 504:
			return "Can't reach the Mac"
		default:
			return fmt.Sprintf("The Mac answered HTTP %d", he.Status)
		}
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne):
		return "Can't reach the Mac"
	default:
		return err.Error()
	}
}
