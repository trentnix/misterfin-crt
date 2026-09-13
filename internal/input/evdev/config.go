package evdev

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Config overrides hardware bindings by device name. Its zero value keeps the
// built-in mappings. Matching profiles apply in order, including after hotplug.
type Config struct {
	Profiles []Profile `json:"profiles"`
}

// Profile matches Linux input device names using a case-sensitive glob.
// Replace removes inherited bindings. Empty actions disable individual inputs.
type Profile struct {
	Match        string                `json:"match"`
	Replace      bool                  `json:"replace,omitempty"`
	Buttons      map[uint16]string     `json:"buttons,omitempty"`
	Axes         map[uint16]Axis       `json:"axes,omitempty"`
	ButtonLabels map[uint16]string     `json:"button_labels,omitempty"`
	AxisLabels   map[uint16]AxisLabels `json:"axis_labels,omitempty"`
}

// AxisLabels names the physical directions of an axis, independently of the
// actions assigned to them. Labels follow bindings when actions are remapped.
type AxisLabels struct {
	Negative string `json:"negative,omitempty"`
	Positive string `json:"positive,omitempty"`
}

// Axis maps either end of an absolute axis to an action. Rest is "center" (the
// default), "minimum", or "maximum", using the range advertised by the driver.
// Press and Release are percentages of travel from rest. Zero selects 25 and
// 15 respectively. Separate thresholds prevent noisy inputs from chattering.
type Axis struct {
	Rest     string `json:"rest,omitempty"`
	Negative string `json:"negative,omitempty"`
	Positive string `json:"positive,omitempty"`
	Press    int    `json:"press,omitempty"`
	Release  int    `json:"release,omitempty"`
}

func (a Axis) thresholds() (int, int) {
	press, release := a.Press, a.Release
	if press == 0 {
		press = 25
	}
	if release == 0 {
		release = 15
	}
	return press, release
}

// Validate rejects misspelled actions, invalid device patterns, and thresholds
// that cannot release reliably. Call it before passing a Config to Read.
func (c Config) Validate() error {
	for i, p := range c.Profiles {
		if _, err := filepath.Match(p.Match, ""); err != nil || p.Match == "" {
			return fmt.Errorf("profile %d: invalid match %q", i+1, p.Match)
		}
		for code, label := range p.ButtonLabels {
			if code > 0x2ff || !validLabel(label) {
				return fmt.Errorf("profile %d: invalid button label %d", i+1, code)
			}
		}
		for code, labels := range p.AxisLabels {
			if code > 0x3f || !validLabel(labels.Negative) || !validLabel(labels.Positive) {
				return fmt.Errorf("profile %d: invalid axis label %d", i+1, code)
			}
		}
		for code, action := range p.Buttons {
			if code > 0x2ff || !validAction(action) {
				return fmt.Errorf("profile %d: invalid button %d action %q", i+1, code, action)
			}
		}
		for code, axis := range p.Axes {
			press, release := axis.thresholds()
			if code > 0x3f || !validAction(axis.Negative) || !validAction(axis.Positive) ||
				(axis.Rest != "" && axis.Rest != "center" && axis.Rest != "minimum" && axis.Rest != "maximum") ||
				(axis.Rest == "minimum" && axis.Negative != "") || (axis.Rest == "maximum" && axis.Positive != "") ||
				release <= 0 || press <= release || press > 100 {
				return fmt.Errorf("profile %d: invalid axis %d", i+1, code)
			}
		}
	}
	return nil
}

func validLabel(label string) bool {
	return len(label) <= 12 && strings.IndexFunc(label, func(r rune) bool { return r < 32 || r > 126 }) < 0
}

func validAction(action string) bool {
	switch action {
	case "", "up", "down", "previous", "next", "open", "back", "select", "retry", "quit", "track-previous", "track-next", "seek-backward", "seek-forward":
		return true
	}
	return false
}

// bindings merges only configuration data. Device discovery adds axis ranges
// separately, keeping matching and validation independent of Linux syscalls.
func (c Config) bindings(name string) Profile {
	result := Profile{Buttons: make(map[uint16]string), Axes: make(map[uint16]Axis), ButtonLabels: make(map[uint16]string), AxisLabels: make(map[uint16]AxisLabels)}
	for _, p := range c.Profiles {
		if match, _ := filepath.Match(p.Match, name); !match {
			continue
		}
		if p.Replace {
			result = Profile{Replace: true, Buttons: make(map[uint16]string), Axes: make(map[uint16]Axis), ButtonLabels: make(map[uint16]string), AxisLabels: make(map[uint16]AxisLabels)}
		}
		for code, action := range p.Buttons {
			result.Buttons[code] = action
		}
		for code, axis := range p.Axes {
			result.Axes[code] = axis
		}
		for code, label := range p.ButtonLabels {
			result.ButtonLabels[code] = label
		}
		for code, label := range p.AxisLabels {
			result.AxisLabels[code] = label
		}
	}
	return result
}
