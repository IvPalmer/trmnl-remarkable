package device

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentifyKnownMachines(t *testing.T) {
	for _, tc := range []struct {
		machine    string
		id         string
		width      int
		colour     bool
		frontlight bool
	}{
		{"reMarkable Ferrari", "rmpp", 1620, true, true},
		{"reMarkable 2.0", "rm2", 1404, false, false},
		{"reMarkable 1.0", "rm1", 1404, false, false},
		{"reMarkable Prototype 1", "rm1", 1404, false, false},
	} {
		p := Identify(tc.machine)
		if p.ID != tc.id || p.Width != tc.width || p.Color != tc.colour || p.Frontlight != tc.frontlight || !p.Known {
			t.Errorf("Identify(%q) = %+v", tc.machine, p)
		}
		if p.Machine != tc.machine {
			t.Errorf("Identify(%q) lost the machine name: %q", tc.machine, p.Machine)
		}
	}
}

func TestIdentifyUnknownMachineStaysUsable(t *testing.T) {
	p := Identify("reMarkable Chiappa")
	if p.Known {
		t.Fatal("an unrecognised machine must not report as known hardware")
	}
	if p.Width == 0 || p.Height == 0 || len(p.Palette()) == 0 {
		t.Fatalf("unknown device left without display defaults: %+v", p)
	}
	if p.Name != "reMarkable Chiappa" {
		t.Fatalf("unknown device should be named after its machine string, got %q", p.Name)
	}
}

func TestPaletteMatchesPanel(t *testing.T) {
	if len(RM2.Palette()) != 16 {
		t.Fatalf("greyscale panels dither to a 16-level ramp, got %d", len(RM2.Palette()))
	}
	colour := PaperPro.Palette()
	if len(colour) == 0 || colour[0] == RM2.Palette()[1] {
		t.Fatal("the colour panel must not fall back to the grey ramp")
	}
}

func TestDetectReadsTheFirstAvailableSource(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent")
	// The device-tree node is NUL-terminated, which must not end up in the name.
	tree := filepath.Join(dir, "model")
	if err := os.WriteFile(tree, []byte("reMarkable 2.0\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DetectIn([]string{missing, tree}); got.ID != "rm2" {
		t.Fatalf("DetectIn fell through to %+v", got)
	}
	if got := DetectIn([]string{missing}); got.Known {
		t.Fatalf("DetectIn invented hardware: %+v", got)
	}
}
