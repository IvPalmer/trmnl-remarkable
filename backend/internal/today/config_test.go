package today

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMissingConfigMeansNotSetUp(t *testing.T) {
	_, err := LoadConfig(filepath.Join(t.TempDir(), "today.json"))
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestLoadConfig(t *testing.T) {
	p := writeFile(t, t.TempDir(), "today.json", `{"gateway_url":"http://mac.test:8090","proxy":"http://127.0.0.1:1055","token_file":"/x/today.token","timezone":"America/Sao_Paulo","sections":["due","mail"]}`)
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.GatewayURL != "http://mac.test:8090" || c.Proxy != "http://127.0.0.1:1055" ||
		c.TokenFile != "/x/today.token" || strings.Join(c.Sections, ",") != "due,mail" {
		t.Fatalf("config = %+v", c)
	}
	if c.Location().String() != "America/Sao_Paulo" {
		t.Fatalf("location = %v", c.Location())
	}
}

func TestLoadConfigRejects(t *testing.T) {
	for name, body := range map[string]string{
		"unknown field": `{"gateway_url":"http://m:1","proxy":"http://127.0.0.1:1055","token_file":"/t","timezone":"UTC","extra":1}`,
		"not http":      `{"gateway_url":"ftp://m:1","proxy":"http://127.0.0.1:1055","token_file":"/t","timezone":"UTC"}`,
		"no host":       `{"gateway_url":"http://","proxy":"http://127.0.0.1:1055","token_file":"/t","timezone":"UTC"}`,
		"no proxy":      `{"gateway_url":"http://m:1","token_file":"/t","timezone":"UTC"}`,
		"socks proxy":   `{"gateway_url":"http://m:1","proxy":"socks5://127.0.0.1:1055","token_file":"/t","timezone":"UTC"}`,
		"no token file": `{"gateway_url":"http://m:1","proxy":"http://127.0.0.1:1055","timezone":"UTC"}`,
		"no timezone":   `{"gateway_url":"http://m:1","proxy":"http://127.0.0.1:1055","token_file":"/t"}`,
		"bad timezone":  `{"gateway_url":"http://m:1","proxy":"http://127.0.0.1:1055","token_file":"/t","timezone":"Mars/Base"}`,
		"not json":      `{`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadConfig(writeFile(t, t.TempDir(), "today.json", body))
			if err == nil || errors.Is(err, ErrNotConfigured) {
				t.Fatalf("err = %v, want a config error", err)
			}
		})
	}
}

func TestReadToken(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
		ok               bool
	}{
		{"minted with a newline", "tok_123-AB\n", "tok_123-AB", true},
		{"windows line ending", "tok\r\n", "tok", true},
		{"no line ending", "tok", "tok", true},
		{"two newlines", "tok\n\n", "", false},
		{"inner space", "to k\n", "", false},
		{"empty", "", "", false},
		{"only a newline", "\n", "", false},
		{"non-ascii", "tök\n", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := ReadToken(writeFile(t, t.TempDir(), "today.token", c.body))
			if c.ok && (err != nil || got != c.want) {
				t.Fatalf("ReadToken = %q, %v; want %q", got, err, c.want)
			}
			if !c.ok && err == nil {
				t.Fatalf("ReadToken = %q, want an error", got)
			}
		})
	}
}

func TestTokenErrorsNeverContainTheToken(t *testing.T) {
	_, err := ReadToken(writeFile(t, t.TempDir(), "today.token", "secret value\n"))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
}
