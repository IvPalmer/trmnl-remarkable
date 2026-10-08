package charger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// supply writes one power_supply entry. An empty value leaves that file out.
func supply(t *testing.T, root, name string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for file, value := range files {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(value+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOnline(t *testing.T) {
	// The battery, as the Paper Pro reports it on the charger: full, yet
	// "Discharging". Only the charger entry is allowed to answer.
	battery := map[string]string{"type": "Battery", "capacity": "100", "status": "Discharging"}
	cases := []struct {
		name     string
		supplies map[string]map[string]string
		want     bool
	}{
		{"online", map[string]map[string]string{
			"battery":  battery,
			"charger0": {"type": "USB", "online": "1"},
		}, true},
		{"offline", map[string]map[string]string{
			"battery":  {"type": "Battery", "capacity": "60", "status": "Discharging"},
			"charger0": {"type": "USB", "online": "0"},
		}, false},
		{"100 percent and Discharging while the charger is online", map[string]map[string]string{
			"battery":  battery,
			"charger0": {"type": "USB", "online": "1"},
		}, true},
		{"100 percent, Discharging and unplugged", map[string]map[string]string{
			"battery":  battery,
			"charger0": {"type": "USB", "online": "0"},
		}, false},
		{"no supplies at all", map[string]map[string]string{}, false},
		{"only a battery", map[string]map[string]string{"battery": {"type": "Battery", "capacity": "100", "status": "Full"}}, false},
		{"a supply without an online file", map[string]map[string]string{"charger0": {"type": "USB"}}, false},
		{"mains", map[string]map[string]string{"ac": {"type": "Mains", "online": "1"}}, true},
		{"USB subtypes", map[string]map[string]string{"usb": {"type": "USB_PD", "online": "1"}}, true},
		{"a marker accessory on a wireless supply is not a charger", map[string]map[string]string{
			"marker":   {"type": "Wireless", "online": "1"},
			"charger0": {"type": "USB", "online": "0"},
		}, false},
		{"an entry with no type is not trusted", map[string]map[string]string{"x": {"online": "1"}}, false},
		{"garbage in the online file", map[string]map[string]string{"charger0": {"type": "USB", "online": "yes"}}, false},
		{"any one online charger counts", map[string]map[string]string{
			"a": {"type": "USB", "online": "0"},
			"b": {"type": "Mains", "online": "1"},
		}, true},
		{"the name is not what matters", map[string]map[string]string{"whatever-chip": {"type": "USB", "online": "1"}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			for name, files := range c.supplies {
				supply(t, root, name, files)
			}
			if got := Online(root); got != c.want {
				t.Fatalf("Online = %t, want %t", got, c.want)
			}
		})
	}
}

func TestOnlineWithAMissingOrEmptyRootIsOffline(t *testing.T) {
	if Online(filepath.Join(t.TempDir(), "absent")) {
		t.Fatal("a missing directory read as online")
	}
	if Online("") {
		t.Fatal("an empty root read as online")
	}
}

func TestReadStampsTheMoment(t *testing.T) {
	root := t.TempDir()
	supply(t, root, "charger0", map[string]string{"type": "USB", "online": "1"})
	at := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	r := Read(root, at)
	if !r.Online || !r.At.Equal(at) {
		t.Fatalf("Read = %+v", r)
	}
	if got := r.AtMillis(); got != at.UnixMilli() {
		t.Fatalf("AtMillis = %d, want %d", got, at.UnixMilli())
	}
	if (Reading{}).AtMillis() != 0 {
		t.Fatal("a reading never taken must stamp 0")
	}
}
