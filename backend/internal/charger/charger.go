// Package charger tells whether the tablet is plugged in, from the kernel's
// power_supply class.
//
// The answer comes from the charger entry, never from the battery: on the Paper
// Pro the battery's status reads "Discharging" at 100 % while the charger is
// online, so a status check misses exactly the case that matters here.
package charger

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Reading is one look at the supplies and the moment it was taken. A consumer
// that is handed a Reading judges its age; a zero At means no reading yet.
type Reading struct {
	Online bool
	At     time.Time
}

// Read looks at the supplies under root and stamps the result with now.
func Read(root string, now time.Time) Reading {
	return Reading{Online: Online(root), At: now}
}

// AtMillis is At as Unix milliseconds, or 0 when no reading was taken. It is
// what the UI is sent: an integer, so the QML does not have to parse a date.
func (r Reading) AtMillis() int64 {
	if r.At.IsZero() {
		return 0
	}
	return r.At.UnixMilli()
}

// Online reports whether any supply under root (normally
// /sys/class/power_supply) is a charger and says it is online. A supply counts
// as a charger by its `type` (Mains or any USB flavour), not by its name, which
// differs between models. Wireless supplies are left out: on the Paper Pro they
// are marker accessories. A missing directory, a supply without an `online`
// file, or anything but "1" reads as not online.
func Online(root string) bool {
	if root == "" {
		return false
	}
	paths, _ := filepath.Glob(filepath.Join(root, "*", "online"))
	for _, p := range paths {
		if !isCharger(readTrimmed(filepath.Join(filepath.Dir(p), "type"))) {
			continue
		}
		if readTrimmed(p) == "1" {
			return true
		}
	}
	return false
}

func isCharger(kind string) bool {
	return kind == "Mains" || strings.HasPrefix(kind, "USB")
}

func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
