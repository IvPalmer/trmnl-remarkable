// Package device identifies which reMarkable the backend is running on. Panel
// geometry, colour capability and the presence of a front light differ between
// the Paper Pro and the reMarkable 1 and 2, so the rest of the backend asks
// here instead of assuming the hardware the project was developed on.
package device

import (
	"os"
	"strings"

	"trmnl-remarkable/backend/internal/config"
)

// Profile describes one supported panel. It is reported to the frontend and
// included in diagnostics, so every field is safe to show the user.
type Profile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Machine    string `json:"machine,omitempty"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Color      bool   `json:"color"`
	Frontlight bool   `json:"frontlight"`
	Known      bool   `json:"known"`
}

var (
	PaperPro = Profile{ID: "rmpp", Name: "reMarkable Paper Pro", Width: 1620, Height: 2160, Color: true, Frontlight: true, Known: true}
	RM2      = Profile{ID: "rm2", Name: "reMarkable 2", Width: 1404, Height: 1872, Known: true}
	RM1      = Profile{ID: "rm1", Name: "reMarkable 1", Width: 1404, Height: 1872, Known: true}
)

// machineSources are read in order. Both files carry the same string on every
// firmware this project supports; the second is the fallback for a kernel that
// does not register a SoC device.
var machineSources = []string{"/sys/devices/soc0/machine", "/proc/device-tree/model"}

// Detect reads the machine name from the running system. An unrecognised
// tablet still yields a usable profile: the installer is what refuses unknown
// hardware, and the backend only needs sensible display defaults.
func Detect() Profile { return DetectIn(machineSources) }

func DetectIn(sources []string) Profile {
	for _, path := range sources {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// The device tree node is NUL-terminated.
		machine := strings.TrimSpace(strings.TrimRight(string(b), "\x00\n"))
		if machine == "" {
			continue
		}
		return Identify(machine)
	}
	return Identify("")
}

// Identify maps a machine name onto a profile. The reMarkable 1 reports two
// different names depending on production batch, and both are in the wild.
func Identify(machine string) Profile {
	machine = strings.TrimSpace(machine)
	var p Profile
	switch machine {
	case "reMarkable Ferrari":
		p = PaperPro
	case "reMarkable 2.0":
		p = RM2
	case "reMarkable 1.0", "reMarkable Prototype 1":
		p = RM1
	default:
		// Fall back to the greyscale geometry shared by every reMarkable that
		// is not a Paper Pro rather than inventing a resolution.
		p = Profile{ID: "unknown", Name: displayName(machine), Width: 1404, Height: 1872}
	}
	p.Machine = machine
	return p
}

func displayName(machine string) string {
	if machine == "" {
		return "unknown reMarkable"
	}
	return machine
}

// greyRamp is the 16-level ramp the reMarkable 1 and 2 panels render. Dithering
// a colour dashboard onto it also performs the colour-to-grey conversion, which
// is why these devices benefit from the same setting the Paper Pro uses for
// banding.
var greyRamp = []string{
	"#000000", "#111111", "#222222", "#333333", "#444444", "#555555", "#666666", "#777777",
	"#888888", "#999999", "#aaaaaa", "#bbbbbb", "#cccccc", "#dddddd", "#eeeeee", "#ffffff",
}

// Palette returns the colours this panel can show, used as the dither target
// when the user has not configured one.
func (p Profile) Palette() []string {
	if p.Color {
		return config.DefaultDitherPalette
	}
	return greyRamp
}
