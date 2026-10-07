package today

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// proxyLog is what the fake proxy saw. Locked: the race detector cannot see
// the ordering a TCP round trip gives.
type proxyLog struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (p *proxyLog) all() []*http.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*http.Request(nil), p.reqs...)
}

// A forward proxy that answers in place of the gateway, recording what it got.
// Every 3xx answer carries a Location, so a client that follows redirects
// would make a second request.
func fakeProxy(t *testing.T, status int, body string) (*httptest.Server, *proxyLog) {
	t.Helper()
	seen := &proxyLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.mu.Lock()
		seen.reqs = append(seen.reqs, r)
		seen.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if status >= 300 && status < 400 {
			w.Header().Set("Location", "http://elsewhere.test/x")
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func gatewayVia(t *testing.T, proxy string) *Gateway {
	t.Helper()
	g, err := NewGateway(Config{GatewayURL: "http://mac.test:8090", Proxy: proxy,
		TokenFile: "/unused", Timezone: "UTC"}, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestRequestsGoThroughTheProxyWithTheBearer(t *testing.T) {
	srv, seen := fakeProxy(t, 200, `{"brief":null}`)
	var out map[string]any
	if err := gatewayVia(t, srv.URL).Get(context.Background(), "/brief", &out); err != nil {
		t.Fatal(err)
	}
	r := seen.all()[0]
	if r.RequestURI != "http://mac.test:8090/brief" || r.Header.Get("Authorization") != "Bearer tok" {
		t.Fatalf("proxy saw %s %s auth=%q", r.Method, r.RequestURI, r.Header.Get("Authorization"))
	}
}

func TestPostSendsJSON(t *testing.T) {
	srv, seen := fakeProxy(t, 200, `{"ok":true}`)
	if err := gatewayVia(t, srv.URL).Post(context.Background(), "/personal/items",
		map[string]string{"op": "tick"}, nil); err != nil {
		t.Fatal(err)
	}
	if r := seen.all()[0]; r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("proxy saw %s %q", r.Method, r.Header.Get("Content-Type"))
	}
}

func TestGatewayErrorsCarryTheirMessage(t *testing.T) {
	srv, _ := fakeProxy(t, 409, `{"error":"no open item matches"}`)
	err := gatewayVia(t, srv.URL).Get(context.Background(), "/personal", nil)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 409 || he.Message != "no open item matches" {
		t.Fatalf("err = %#v", err)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	srv, seen := fakeProxy(t, 302, `{}`)
	err := gatewayVia(t, srv.URL).Get(context.Background(), "/brief", nil)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 302 || len(seen.all()) != 1 {
		t.Fatalf("err = %v after %d requests", err, len(seen.all()))
	}
}

func TestARefusedProxyMeansTailscaleIsDown(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	err = gatewayVia(t, "http://"+addr).Get(context.Background(), "/brief", nil)
	if !errors.Is(err, ErrTailscaleDown) {
		t.Fatalf("err = %v, want ErrTailscaleDown", err)
	}
}

func TestUserMessages(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w (refused)", ErrTailscaleDown), "Tailscale isn't running on the tablet"},
		{&HTTPError{Status: 401, Message: "bad token"}, "Tablet not authorised. Run rm-today-setup."},
		{&HTTPError{Status: 403, Message: "peer not allowed"}, "The Mac doesn't recognise this tablet yet. Try again in a minute."},
		{&HTTPError{Status: 403, Message: "forbidden"}, "Tablet not authorised. Run rm-today-setup."},
		{&HTTPError{Status: 403}, "Tablet not authorised. Run rm-today-setup."},
		{&HTTPError{Status: 409, Message: "file changed, retry"}, "file changed, retry"},
		{&HTTPError{Status: 502}, "Can't reach the Mac"},
		{&HTTPError{Status: 418}, "The Mac answered HTTP 418"},
		{context.DeadlineExceeded, "Can't reach the Mac"},
		// {ErrNoActions, "this section has no actions"}, // restored in Task 4
	} {
		if got := UserMessage(c.err); got != c.want {
			t.Errorf("UserMessage(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}
