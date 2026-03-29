//go:build darwin

package guiautomation

import (
	"fmt"
	"os/exec"
	"strings"
)

// darwinInputSimulator implements InputSimulator on macOS via python3 + Quartz CGEvent API.
type darwinInputSimulator struct{}

// NewInputSimulator creates a macOS InputSimulator backed by CGEvent.
func NewInputSimulator() InputSimulator { return &darwinInputSimulator{} }

// runPython executes a python3 script and returns any error with combined output.
func runPython(script string) error {
	cmd := exec.Command("python3", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("input simulation failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *darwinInputSimulator) Click(x, y int) error {
	script := fmt.Sprintf(`
import Quartz
p = Quartz.CGPointMake(%d, %d)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseDown, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseUp, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
`, x, y)
	return runPython(script)
}

func (d *darwinInputSimulator) RightClick(x, y int) error {
	script := fmt.Sprintf(`
import Quartz
p = Quartz.CGPointMake(%d, %d)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventRightMouseDown, p, Quartz.kCGMouseButtonRight)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventRightMouseUp, p, Quartz.kCGMouseButtonRight)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
`, x, y)
	return runPython(script)
}

func (d *darwinInputSimulator) DoubleClick(x, y int) error {
	script := fmt.Sprintf(`
import Quartz
p = Quartz.CGPointMake(%d, %d)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseDown, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventSetIntegerValueField(e, Quartz.kCGMouseEventClickState, 1)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseUp, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventSetIntegerValueField(e, Quartz.kCGMouseEventClickState, 1)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseDown, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventSetIntegerValueField(e, Quartz.kCGMouseEventClickState, 2)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseUp, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventSetIntegerValueField(e, Quartz.kCGMouseEventClickState, 2)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
`, x, y)
	return runPython(script)
}

func (d *darwinInputSimulator) Type(text string) error {
	// Escape backslashes and single quotes for Python string literal.
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(text)
	script := fmt.Sprintf(`
import Quartz, time
text = '%s'
for ch in text:
    e = Quartz.CGEventCreateKeyboardEvent(None, 0, True)
    Quartz.CGEventKeyboardSetUnicodeString(e, len(ch), ch)
    Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
    e = Quartz.CGEventCreateKeyboardEvent(None, 0, False)
    Quartz.CGEventKeyboardSetUnicodeString(e, len(ch), ch)
    Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
    time.sleep(0.01)
`, escaped)
	return runPython(script)
}

// cgKeyMap maps common key names to macOS CGEvent virtual keycodes.
var cgKeyMap = map[string]int{
	"return": 36, "enter": 36, "tab": 48, "space": 49, "backspace": 51, "delete": 51,
	"escape": 53, "esc": 53,
	"command": 55, "cmd": 55, "shift": 56, "capslock": 57, "option": 58,
	"alt": 58, "control": 59, "ctrl": 59,
	"rightshift": 60, "rightoption": 61, "rightcontrol": 62, "rightcommand": 54,
	"fn": 63,
	"f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97,
	"f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111,
	"home": 115, "end": 119, "pageup": 116, "pagedown": 121,
	"left": 123, "right": 124, "down": 125, "up": 126,
	"a": 0, "b": 11, "c": 8, "d": 2, "e": 14, "f": 3, "g": 5, "h": 4,
	"i": 34, "j": 38, "k": 40, "l": 37, "m": 46, "n": 45, "o": 31,
	"p": 35, "q": 12, "r": 15, "s": 1, "t": 17, "u": 32, "v": 9,
	"w": 13, "x": 7, "y": 16, "z": 6,
	"0": 29, "1": 18, "2": 19, "3": 20, "4": 21, "5": 23, "6": 22,
	"7": 26, "8": 28, "9": 25,
	"-": 27, "=": 24, "[": 33, "]": 30, `\`: 42, ";": 41, "'": 39,
	",": 43, ".": 47, "/": 44, "`": 50,
}

func resolveCGKeycode(key string) (int, error) {
	k := strings.ToLower(strings.TrimSpace(key))
	if code, ok := cgKeyMap[k]; ok {
		return code, nil
	}
	return 0, fmt.Errorf("unknown key: %q", key)
}

func (d *darwinInputSimulator) KeyCombo(keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	var downs, ups []string
	for _, k := range keys {
		code, err := resolveCGKeycode(k)
		if err != nil {
			return err
		}
		downs = append(downs, fmt.Sprintf(
			"e = Quartz.CGEventCreateKeyboardEvent(None, %d, True); Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)", code))
		// Release in reverse order.
		ups = append([]string{fmt.Sprintf(
			"e = Quartz.CGEventCreateKeyboardEvent(None, %d, False); Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)", code)}, ups...)
	}
	script := "import Quartz\n" + strings.Join(downs, "\n") + "\n" + strings.Join(ups, "\n")
	return runPython(script)
}

func (d *darwinInputSimulator) Scroll(x, y, deltaX, deltaY int) error {
	if deltaX == 0 && deltaY == 0 {
		return nil
	}
	// Move cursor to position first, then scroll.
	script := fmt.Sprintf(`
import Quartz
p = Quartz.CGPointMake(%d, %d)
move = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventMouseMoved, p, Quartz.kCGMouseButtonLeft)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, move)
scroll = Quartz.CGEventCreateScrollWheelEvent(None, Quartz.kCGScrollEventUnitLine, 2, %d, %d)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, scroll)
`, x, y, deltaY, deltaX)
	return runPython(script)
}

func (d *darwinInputSimulator) DragDrop(fromX, fromY, toX, toY int) error {
	script := fmt.Sprintf(`
import Quartz, time
src = Quartz.CGPointMake(%d, %d)
dst = Quartz.CGPointMake(%d, %d)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseDown, src, Quartz.kCGMouseButtonLeft)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
time.sleep(0.05)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseDragged, dst, Quartz.kCGMouseButtonLeft)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
time.sleep(0.05)
e = Quartz.CGEventCreateMouseEvent(None, Quartz.kCGEventLeftMouseUp, dst, Quartz.kCGMouseButtonLeft)
Quartz.CGEventPost(Quartz.kCGHIDEventTap, e)
`, fromX, fromY, toX, toY)
	return runPython(script)
}
