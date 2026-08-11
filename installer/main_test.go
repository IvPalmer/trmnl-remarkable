package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseKeyValues(t *testing.T) {
	got := parseKeyValues("model=reMarkable Ferrari\nos=3.27.3.0\ninstalled=yes\nactive=no\n")
	if got["model"] != "reMarkable Ferrari" || got["installed"] != "yes" || got["active"] != "no" {
		t.Fatalf("unexpected values: %#v", got)
	}
}

func TestIdentifyDevice(t *testing.T) {
	for _, tc := range []struct {
		values map[string]string
		name   string
		arch   string
		known  bool
	}{
		{map[string]string{"machine": "reMarkable Ferrari", "model": "reMarkable Ferrari"}, "reMarkable Paper Pro", "aarch64", true},
		{map[string]string{"machine": "reMarkable 2.0", "model": "reMarkable 2.0"}, "reMarkable 2", "arm32", true},
		{map[string]string{"machine": "reMarkable Prototype 1"}, "reMarkable 1", "arm32", true},
		// A kernel without a SoC device leaves only the device-tree model.
		{map[string]string{"machine": "", "model": "reMarkable 2.0"}, "reMarkable 2", "arm32", true},
		{map[string]string{"machine": "reMarkable Chiappa"}, "", "", false},
	} {
		device, ok := identifyDevice(tc.values)
		if ok != tc.known {
			t.Fatalf("identifyDevice(%v) known = %t", tc.values, ok)
		}
		if ok && (device.Name != tc.name || device.PayloadArch != tc.arch) {
			t.Fatalf("identifyDevice(%v) = %+v", tc.values, device)
		}
	}
}

func TestFirmwareWindowsPerDevice(t *testing.T) {
	paperPro := supportedDevices["reMarkable Ferrari"]
	if len(paperPro.Firmware) != 2 {
		t.Fatalf("the Paper Pro window changed unexpectedly: %v", paperPro.Firmware)
	}
	// The reMarkable 1 stopped receiving releases, so it must accept the older
	// line it shipped with; the tablets still being updated must not.
	rm1 := supportedDevices["reMarkable 1.0"]
	if !strings.Contains(strings.Join(rm1.Firmware, " "), "3.20.") {
		t.Fatalf("the reMarkable 1 window excludes its last firmware: %v", rm1.Firmware)
	}
	for _, prefix := range supportedDevices["reMarkable 2.0"].Firmware {
		if prefix == "3.20." {
			t.Fatal("the reMarkable 2 window should track the validated firmware only")
		}
	}
}

func TestPayloadFileApplies(t *testing.T) {
	shared := []string{"install-device-runtime.sh", "licenses/XOVI-LICENSE", "manifest.json"}
	for _, name := range shared {
		if !payloadFileApplies(name, "arm32") || !payloadFileApplies(name, "aarch64") {
			t.Fatalf("%s should be uploaded to every device", name)
		}
	}
	if !payloadFileApplies("appload/qtfb-shim-32bit-arm32.so", "arm32") {
		t.Fatal("the 32-bit shim of the arm32 payload must be uploaded to a reMarkable 2")
	}
	if payloadFileApplies("appload/qtfb-shim-32bit-aarch64.so", "arm32") {
		t.Fatal("aarch64 files must not be uploaded to a 32-bit tablet")
	}
	if payloadFileApplies("trmnl-remarkable-device-arm32.tar.gz", "aarch64") {
		t.Fatal("arm32 files must not be uploaded to a Paper Pro")
	}
}

func TestVerifyPayload(t *testing.T) {
	dir := t.TempDir()
	data := []byte("safe payload")
	if err := os.WriteFile(filepath.Join(dir, "file.bin"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	m := payloadManifest{Version: "1.0.0", Files: map[string]string{"file.bin": hex.EncodeToString(sum[:])}}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyPayload(dir); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
	m.Files["../escape"] = hex.EncodeToString(sum[:])
	b, _ = json.Marshal(m)
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o600)
	if _, err := verifyPayload(dir); err == nil {
		t.Fatal("unsafe payload path accepted")
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("a'b"); got != `'a'"'"'b'` {
		t.Fatalf("unexpected shell quote: %s", got)
	}
}

func TestLoopbackOnlyRejectsRebindingHosts(t *testing.T) {
	handler := loopbackOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	allowed := []string{"127.0.0.1:53017", "localhost:53017", "[::1]:53017", "127.0.0.1"}
	for _, host := range allowed {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = host
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusTeapot {
			t.Fatalf("loopback host %q was rejected with %d", host, recorder.Code)
		}
	}
	rebound := []string{"attacker.example:53017", "trmnl.example.com", "127.0.0.1.attacker.example:53017", "10.0.0.5:53017"}
	for _, host := range rebound {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = host
		handler.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("non-loopback host %q was accepted with %d", host, recorder.Code)
		}
	}
}

func TestIsLoopbackHostGuardsTheListenOverride(t *testing.T) {
	for _, address := range []string{"127.0.0.1:0", "localhost:8080", "[::1]:8080", "127.4.5.6:0"} {
		if !isLoopbackHost(address) {
			t.Fatalf("loopback listen address %q was refused", address)
		}
	}
	for _, address := range []string{"0.0.0.0:8080", ":8080", "192.168.1.10:8080", "[::]:8080", "tablet.local:8080"} {
		if isLoopbackHost(address) {
			t.Fatalf("non-loopback listen address %q was accepted", address)
		}
	}
}

func TestCSRFGuardRejectsWrongAndMissingTokens(t *testing.T) {
	s := &server{csrf: "correct-token"}
	for _, token := range []string{"", "wrong-token", "correct-toke", "correct-tokenn"} {
		req := httptest.NewRequest("POST", "/api/preflight", strings.NewReader("{}"))
		if token != "" {
			req.Header.Set("X-TRMNL-CSRF", token)
		}
		recorder := httptest.NewRecorder()
		var c credentials
		if s.decode(recorder, req, &c) {
			t.Fatalf("token %q was accepted", token)
		}
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("token %q produced status %d, want 403", token, recorder.Code)
		}
	}
	req := httptest.NewRequest("POST", "/api/preflight", strings.NewReader(`{"host":"10.11.99.1"}`))
	req.Header.Set("X-TRMNL-CSRF", "correct-token")
	var c credentials
	if !s.decode(httptest.NewRecorder(), req, &c) {
		t.Fatal("the correct token was rejected")
	}
	if c.Host != "10.11.99.1" {
		t.Fatalf("decoded host = %q", c.Host)
	}
}

func TestDeviceFailureFallsBackToTransportError(t *testing.T) {
	if got := deviceFailure("  recovery verification failed  ", errors.New("exit status 40")); got != "recovery verification failed" {
		t.Fatalf("device output was not preferred: %q", got)
	}
	got := deviceFailure("   ", errors.New("exit status 1"))
	if got == "" || !strings.Contains(got, "exit status 1") {
		t.Fatalf("silent failure produced %q", got)
	}
}

func TestInstallerPageRenders(t *testing.T) {
	s := &server{csrf: "test-token"}
	req := httptest.NewRequest("GET", "/", nil)
	recorder := httptest.NewRecorder()
	s.index(recorder, req)
	if recorder.Code != 200 {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, expected := range []string{"TRMNL for reMarkable", "Find my tablet", "Developer Mode", "Reactivate after reboot", "Uninstall and erase data", "test-token"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("installer page missing %q", expected)
		}
	}
	if strings.Contains(body, "{{CSRF}}") || strings.Contains(body, "{{VERSION}}") {
		t.Fatal("installer template placeholders were not replaced")
	}
}

// A remembered key must win over whatever the page echoes back, except when the
// user has explicitly accepted a change (Developer Mode resets the host key).
func TestExpectedFingerprintPrefersRememberedKey(t *testing.T) {
	s := &server{known: &knownHosts{Hosts: map[string]string{"10.11.99.1:22": "SHA256:stored"}}}

	if got := s.expectedFingerprint(credentials{Host: "10.11.99.1", Fingerprint: "SHA256:attacker"}); got != "SHA256:stored" {
		t.Fatalf("expectedFingerprint = %q, want the remembered key", got)
	}
	if got := s.expectedFingerprint(credentials{Host: "10.11.99.1", Fingerprint: "SHA256:new", AcceptNewKey: true}); got != "SHA256:new" {
		t.Fatalf("an accepted key change was ignored: %q", got)
	}
	if got := s.expectedFingerprint(credentials{Host: "192.168.1.50", Fingerprint: "SHA256:first"}); got != "SHA256:first" {
		t.Fatalf("an unseen host should trust on first use, got %q", got)
	}
}

func TestKnownHostsRoundTripAndNormalisation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known.json")
	k := loadKnownHosts(path)
	if _, ok := k.Lookup("10.11.99.1"); ok {
		t.Fatal("an empty record reported a known host")
	}
	k.Remember("10.11.99.1", "SHA256:abc")

	// A bare host, an explicit port, and different casing are the same tablet.
	reloaded := loadKnownHosts(path)
	for _, host := range []string{"10.11.99.1", "10.11.99.1:22", "  10.11.99.1  "} {
		got, ok := reloaded.Lookup(host)
		if !ok || got != "SHA256:abc" {
			t.Fatalf("Lookup(%q) = %q, %v", host, got, ok)
		}
	}
	if _, ok := reloaded.Lookup("10.11.99.1:2222"); ok {
		t.Fatal("a different port matched the stored key")
	}
	// An empty fingerprint must never overwrite a good record.
	reloaded.Remember("10.11.99.1", "")
	if got, _ := loadKnownHosts(path).Lookup("10.11.99.1"); got != "SHA256:abc" {
		t.Fatalf("record was clobbered: %q", got)
	}
}
