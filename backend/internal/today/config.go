// Package today is the Today view: the user's widget screen, read from their
// session gateway through the tablet's Tailscale proxy. The gateway decides
// what it shows; see "Changing what Today shows" in docs/TODAY.md.
package today

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"time"
	_ "time/tzdata" // the tablet runs UTC; its zoneinfo is not relied on
)

// Config is ~/.config/trmnl-remarkable/today.json.
type Config struct {
	GatewayURL string   `json:"gateway_url"`
	Proxy      string   `json:"proxy"`
	TokenFile  string   `json:"token_file"`
	Timezone   string   `json:"timezone"`
	Sections   []string `json:"sections,omitempty"`
}

// ErrNotConfigured means there is no today.json: Today is simply off.
var ErrNotConfigured = errors.New("today is not set up")

func LoadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, ErrNotConfigured
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("today.json: %w", err)
	}
	return c, c.validate()
}

func (c Config) validate() error {
	u, err := url.Parse(c.GatewayURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("today.json: gateway_url must be an http(s) URL")
	}
	if c.Proxy == "" {
		return errors.New("today.json: proxy is required")
	}
	p, err := url.Parse(c.Proxy)
	if err != nil || p.Scheme != "http" || p.Host == "" {
		return errors.New("today.json: proxy must be an http URL")
	}
	if c.TokenFile == "" {
		return errors.New("today.json: token_file is required")
	}
	if c.Timezone == "" {
		return errors.New("today.json: timezone is required")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("today.json: unknown timezone %q", c.Timezone)
	}
	return nil
}

// Location is the operator's zone: "today" and every displayed time use it.
func (c Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// ReadToken reads the gateway bearer. The minting tool ends it with a
// newline; one line ending is stripped, and anything that is not a visible
// ASCII character is refused, so a bad file never becomes a malformed
// Authorization header. Errors never include the file's contents.
func ReadToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	if s == "" {
		return "", errors.New("today.token is empty")
	}
	for _, r := range s {
		if r < 0x21 || r > 0x7e {
			return "", errors.New("today.token contains a character a bearer token cannot")
		}
	}
	return s, nil
}
