//go:build linux

package guiautomation

import (
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// linuxInputSimulator implements InputSimulator on Linux via xdotool.
type linuxInputSimulator struct{}

// NewInputSimulator creates a Linux InputSimulator backed by xdotool.
func NewInputSimulator() InputSimulator { return &linuxInputSimulator{} }

// runXdotool executes an xdotool command with the given arguments.
func runXdotool(args ...string) error {
	cmd := exec.Command("xdotool", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("xdotool %s failed: %w (output: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (l *linuxInputSimulator) Click(x, y int) error {
	if err := runXdotool("mousemove", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
		return err
	}
	return runXdotool("click", "1")
}

func (l *linuxInputSimulator) RightClick(x, y int) error {
	if err := runXdotool("mousemove", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
		return err
	}
	return runXdotool("click", "3")
}

func (l *linuxInputSimulator) DoubleClick(x, y int) error {
	if err := runXdotool("mousemove", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
		return err
	}
	return runXdotool("click", "--repeat", "2", "--delay", "50", "1")
}

func (l *linuxInputSimulator) Type(text string) error {
	return runXdotool("type", "--clearmodifiers", text)
}

// xdotoolKeyMap maps common key names to xdotool key identifiers.
var xdotoolKeyMap = map[string]string{
	"ctrl": "ctrl", "control": "ctrl",
	"alt": "alt", "shift": "shift",
	"win": "super", "super": "super", "meta": "super",
	"enter": "Return", "return": "Return",
	"tab": "Tab", "space": "space",
	"backspace": "BackSpace", "delete": "Delete", "del": "Delete",
	"escape": "Escape", "esc": "Escape",
	"insert": "Insert",
	"home": "Home", "end": "End",
	"pageup": "Prior", "pagedown": "Next",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"printscreen": "Print",
	"capslock": "Caps_Lock",
	"f1": "F1", "f2": "F2", "f3": "F3", "f4": "F4",
	"f5": "F5", "f6": "F6", "f7": "F7", "f8": "F8",
	"f9": "F9", "f10": "F10", "f11": "F11", "f12": "F12",
}

// resolveXdotoolKey maps a key name to its xdotool equivalent.
func resolveXdotoolKey(key string) (string, error) {
	k := strings.ToLower(strings.TrimSpace(key))
	if mapped, ok := xdotoolKeyMap[k]; ok {
		return mapped, nil
	}
	// Single character keys (letters and digits) are passed as-is.
	if len(k) == 1 {
		c := k[0]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			return k, nil
		}
	}
	return "", fmt.Errorf("unknown key: %q", key)
}

func (l *linuxInputSimulator) KeyCombo(keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	mapped := make([]string, 0, len(keys))
	for _, k := range keys {
		mk, err := resolveXdotoolKey(k)
		if err != nil {
			return err
		}
		mapped = append(mapped, mk)
	}
	// xdotool key expects "key1+key2+..." format.
	return runXdotool("key", strings.Join(mapped, "+"))
}

func (l *linuxInputSimulator) Scroll(x, y, deltaX, deltaY int) error {
	if deltaY == 0 {
		return nil
	}
	if err := runXdotool("mousemove", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
		return err
	}
	// xdotool: button 4 = scroll up, button 5 = scroll down.
	button := "5" // down
	if deltaY > 0 {
		button = "4" // up
	}
	clicks := int(math.Abs(float64(deltaY)))
	if clicks == 0 {
		clicks = 1
	}
	return runXdotool("click", "--repeat", strconv.Itoa(clicks), button)
}

func (l *linuxInputSimulator) DragDrop(fromX, fromY, toX, toY int) error {
	if err := runXdotool("mousemove", strconv.Itoa(fromX), strconv.Itoa(fromY)); err != nil {
		return err
	}
	if err := runXdotool("mousedown", "1"); err != nil {
		return err
	}
	if err := runXdotool("mousemove", strconv.Itoa(toX), strconv.Itoa(toY)); err != nil {
		return err
	}
	return runXdotool("mouseup", "1")
}
