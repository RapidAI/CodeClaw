//go:build !windows

package remote

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Stub for Windows — never reached on non-Windows platforms,
// but the compiler needs it to exist.

func enumDisplaysWindows() ([]DisplayInfo, error) {
	return nil, fmt.Errorf("enumDisplaysWindows: not available on this platform")
}

func buildMultiMonitorScreenshotWindows() string       { return "" }
func buildSingleMonitorScreenshotWindows(_ int) string { return "" }

// enumDisplaysDarwin enumerates all connected displays on macOS by running a
// python3 one-liner that uses Quartz.CGGetActiveDisplayList and
// Quartz.CGDisplayBounds to output JSON, then parses the result into
// []DisplayInfo.
func enumDisplaysDarwin() ([]DisplayInfo, error) {
	pyScript := `
import json, Quartz
err, ids, cnt = Quartz.CGGetActiveDisplayList(32, None, None)
if err != 0:
    raise RuntimeError("CGGetActiveDisplayList failed: %d" % err)
result = []
for i, did in enumerate(ids):
    b = Quartz.CGDisplayBounds(did)
    result.append({
        "index": i,
        "name": "Display %d" % did,
        "x": int(b.origin.x),
        "y": int(b.origin.y),
        "width": int(b.size.width),
        "height": int(b.size.height),
        "scale": 1.0,
        "primary": (i == 0),
    })
print(json.dumps(result))
`
	cmd := exec.Command("python3", "-c", pyScript)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("enumDisplaysDarwin: python3 failed: %w", err)
	}

	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		return nil, fmt.Errorf("enumDisplaysDarwin: empty output from python3")
	}

	var raw []struct {
		Index   int     `json:"index"`
		Name    string  `json:"name"`
		X       int     `json:"x"`
		Y       int     `json:"y"`
		Width   int     `json:"width"`
		Height  int     `json:"height"`
		Scale   float64 `json:"scale"`
		Primary bool    `json:"primary"`
	}
	if err := json.Unmarshal([]byte(outStr), &raw); err != nil {
		return nil, fmt.Errorf("enumDisplaysDarwin: json parse failed: %w (output: %s)", err, outStr)
	}

	displays := make([]DisplayInfo, len(raw))
	for i, r := range raw {
		displays[i] = DisplayInfo{
			Index:   r.Index,
			Name:    r.Name,
			X:       r.X,
			Y:       r.Y,
			Width:   r.Width,
			Height:  r.Height,
			Scale:   r.Scale,
			Primary: r.Primary,
		}
	}
	if len(displays) == 0 {
		return nil, fmt.Errorf("enumDisplaysDarwin: no displays found")
	}
	return displays, nil
}

// buildMultiMonitorScreenshotDarwin returns a shell command that uses
// screencapture -x to capture all monitors (screencapture already handles
// multi-monitor natively on macOS) and base64 encodes the result.
func buildMultiMonitorScreenshotDarwin() string {
	return `tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); ` +
		`trap "rm -f \"$tmpfile\"" EXIT; ` +
		`screencapture -x "$tmpfile" 2>/dev/null && ` +
		`base64 -i "$tmpfile"`
}

// buildSingleMonitorScreenshotDarwin returns a shell command that uses python3
// with Quartz CGGetActiveDisplayList to get the display ID at the given index,
// then CGDisplayCreateImage to capture just that display, save to a temp file,
// and base64 encode.
func buildSingleMonitorScreenshotDarwin(screenIndex int) string {
	return fmt.Sprintf(`tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); `+
		`trap "rm -f \"$tmpfile\"" EXIT; `+
		`python3 -c "`+
		`import sys, Quartz`+"\n"+
		`from Foundation import NSURL`+"\n"+
		`idx = %d`+"\n"+
		`err, ids, cnt = Quartz.CGGetActiveDisplayList(32, None, None)`+"\n"+
		`if err != 0:`+"\n"+
		`    print('CGGetActiveDisplayList failed', file=sys.stderr); sys.exit(1)`+"\n"+
		`if idx < 0 or idx >= len(ids):`+"\n"+
		`    print('screen index %%d out of range: %%d display(s) available' %% (idx, len(ids)), file=sys.stderr); sys.exit(1)`+"\n"+
		`image = Quartz.CGDisplayCreateImage(ids[idx])`+"\n"+
		`if image is None:`+"\n"+
		`    print('CGDisplayCreateImage failed', file=sys.stderr); sys.exit(1)`+"\n"+
		`url = NSURL.fileURLWithPath_(sys.argv[1])`+"\n"+
		`dest = Quartz.CGImageDestinationCreateWithURL(url, 'public.png', 1, None)`+"\n"+
		`if dest is None:`+"\n"+
		`    print('CGImageDestinationCreateWithURL failed', file=sys.stderr); sys.exit(1)`+"\n"+
		`Quartz.CGImageDestinationAddImage(dest, image, None)`+"\n"+
		`Quartz.CGImageDestinationFinalize(dest)`+"\n"+
		`" "$tmpfile" && `+
		`base64 -i "$tmpfile"`, screenIndex)
}

// --- Linux implementations ---

// enumDisplaysLinux enumerates connected displays on Linux by trying two
// approaches in order:
//  1. xrandr --query (works on X11 and XWayland)
//  2. python3 fallback reading /sys/class/drm or wlr-randr (Wayland native)
//
// If both fail, returns a single primary display with reasonable defaults.
func enumDisplaysLinux() ([]DisplayInfo, error) {
	// Approach 1: try xrandr
	displays, err := enumDisplaysLinuxXrandr()
	if err == nil && len(displays) > 0 {
		return displays, nil
	}

	// Approach 2: try python3 fallback for Wayland / /sys/class/drm
	displays, err = enumDisplaysLinuxPython()
	if err == nil && len(displays) > 0 {
		return displays, nil
	}

	// Fallback: return a single primary display with reasonable defaults
	return []DisplayInfo{
		{Index: 0, Name: "Primary", X: 0, Y: 0, Width: 1920, Height: 1080, Scale: 1.0, Primary: true},
	}, nil
}

// enumDisplaysLinuxXrandr parses `xrandr --query` output to extract connected
// displays with their geometry. Example line:
//
//	HDMI-1 connected primary 1920x1080+0+0 (normal left inverted right x axis y axis) 527mm x 296mm
func enumDisplaysLinuxXrandr() ([]DisplayInfo, error) {
	cmd := exec.Command("xrandr", "--query")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("xrandr failed: %w", err)
	}

	var displays []DisplayInfo
	idx := 0
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, " connected") {
			continue
		}
		// Parse geometry like "1920x1080+0+0" from the line
		name, width, height, x, y, primary, ok := parseXrandrConnectedLine(line)
		if !ok {
			continue
		}
		displays = append(displays, DisplayInfo{
			Index:   idx,
			Name:    name,
			X:       x,
			Y:       y,
			Width:   width,
			Height:  height,
			Scale:   1.0,
			Primary: primary,
		})
		idx++
	}
	if len(displays) == 0 {
		return nil, fmt.Errorf("xrandr: no connected displays found")
	}
	return displays, nil
}

// parseXrandrConnectedLine parses a single xrandr output line for a connected
// display. Returns the output name, dimensions, position, primary flag, and
// whether parsing succeeded.
//
// Example lines:
//
//	HDMI-1 connected primary 1920x1080+0+0 ...
//	DP-2 connected 2560x1440+1920+0 ...
func parseXrandrConnectedLine(line string) (name string, width, height, x, y int, primary bool, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return
	}
	name = fields[0]
	// fields[1] should be "connected"
	searchStart := 2
	if len(fields) > 3 && fields[2] == "primary" {
		primary = true
		searchStart = 3
	}
	// Find the geometry token matching WxH+X+Y pattern
	for i := searchStart; i < len(fields); i++ {
		w, h, px, py, parsed := parseGeometry(fields[i])
		if parsed {
			width, height, x, y = w, h, px, py
			ok = true
			return
		}
	}
	return
}

// parseGeometry parses a geometry string like "1920x1080+0+0" into
// width, height, x, y. Returns false if the string doesn't match.
func parseGeometry(s string) (width, height, x, y int, ok bool) {
	n, err := fmt.Sscanf(s, "%dx%d+%d+%d", &width, &height, &x, &y)
	if err != nil || n != 4 {
		return 0, 0, 0, 0, false
	}
	if width <= 0 || height <= 0 {
		return 0, 0, 0, 0, false
	}
	ok = true
	return
}

// enumDisplaysLinuxPython uses python3 to enumerate displays via
// /sys/class/drm or wlr-randr as a fallback for Wayland environments.
func enumDisplaysLinuxPython() ([]DisplayInfo, error) {
	pyScript := `
import json, subprocess, os, re, sys

def try_wlr_randr():
    try:
        out = subprocess.check_output(["wlr-randr"], stderr=subprocess.DEVNULL, timeout=5).decode()
        displays = []
        idx = 0
        current_name = None
        for line in out.splitlines():
            line_stripped = line.strip()
            if not line.startswith(" ") and not line.startswith("\t"):
                current_name = line_stripped.split()[0] if line_stripped else None
            elif current_name and "current" in line_stripped:
                m = re.search(r'(\d+)x(\d+)\s+px.*?(\d+\.\d+)\s+Hz', line_stripped)
                if m:
                    w, h = int(m.group(1)), int(m.group(2))
                    displays.append({
                        "index": idx, "name": current_name,
                        "x": 0, "y": 0, "width": w, "height": h,
                        "scale": 1.0, "primary": idx == 0,
                    })
                    idx += 1
        if displays:
            return displays
    except Exception:
        pass
    return None

def try_drm():
    drm_path = "/sys/class/drm"
    if not os.path.isdir(drm_path):
        return None
    displays = []
    idx = 0
    for entry in sorted(os.listdir(drm_path)):
        status_path = os.path.join(drm_path, entry, "status")
        modes_path = os.path.join(drm_path, entry, "modes")
        if not os.path.isfile(status_path):
            continue
        try:
            with open(status_path) as f:
                if f.read().strip() != "connected":
                    continue
        except Exception:
            continue
        w, h = 1920, 1080
        try:
            with open(modes_path) as f:
                first_mode = f.readline().strip()
                m = re.match(r'(\d+)x(\d+)', first_mode)
                if m:
                    w, h = int(m.group(1)), int(m.group(2))
        except Exception:
            pass
        displays.append({
            "index": idx, "name": entry,
            "x": 0, "y": 0, "width": w, "height": h,
            "scale": 1.0, "primary": idx == 0,
        })
        idx += 1
    if displays:
        return displays
    return None

result = try_wlr_randr() or try_drm()
if result:
    print(json.dumps(result))
else:
    sys.exit(1)
`
	cmd := exec.Command("python3", "-c", pyScript)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("enumDisplaysLinuxPython: python3 failed: %w", err)
	}

	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		return nil, fmt.Errorf("enumDisplaysLinuxPython: empty output")
	}

	var raw []struct {
		Index   int     `json:"index"`
		Name    string  `json:"name"`
		X       int     `json:"x"`
		Y       int     `json:"y"`
		Width   int     `json:"width"`
		Height  int     `json:"height"`
		Scale   float64 `json:"scale"`
		Primary bool    `json:"primary"`
	}
	if err := json.Unmarshal([]byte(outStr), &raw); err != nil {
		return nil, fmt.Errorf("enumDisplaysLinuxPython: json parse failed: %w (output: %s)", err, outStr)
	}

	displays := make([]DisplayInfo, len(raw))
	for i, r := range raw {
		displays[i] = DisplayInfo{
			Index:   r.Index,
			Name:    r.Name,
			X:       r.X,
			Y:       r.Y,
			Width:   r.Width,
			Height:  r.Height,
			Scale:   r.Scale,
			Primary: r.Primary,
		}
	}
	if len(displays) == 0 {
		return nil, fmt.Errorf("enumDisplaysLinuxPython: no displays found")
	}
	return displays, nil
}

// buildMultiMonitorScreenshotLinux returns a shell command that captures all
// monitors into a single screenshot. It tries multiple tools in order:
//  1. scrot (X11 — captures entire virtual desktop by default)
//  2. grim (Wayland — captures all outputs by default)
//  3. gnome-screenshot -f (GNOME fallback)
//  4. import -window root (ImageMagick fallback)
//
// The result is base64 encoded.
func buildMultiMonitorScreenshotLinux() string {
	return `tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); ` +
		`trap "rm -f \"$tmpfile\"" EXIT; ` +
		`captured=false; ` +
		// Try scrot first (X11, captures entire virtual desktop)
		`if command -v scrot >/dev/null 2>&1; then ` +
		`scrot "$tmpfile" 2>/dev/null && [ -s "$tmpfile" ] && captured=true; fi; ` +
		// Try grim (Wayland, captures all outputs)
		`if [ "$captured" = "false" ] && command -v grim >/dev/null 2>&1; then ` +
		`grim "$tmpfile" 2>/dev/null && [ -s "$tmpfile" ] && captured=true; fi; ` +
		// Try gnome-screenshot
		`if [ "$captured" = "false" ] && command -v gnome-screenshot >/dev/null 2>&1; then ` +
		`gnome-screenshot -f "$tmpfile" 2>/dev/null && [ -s "$tmpfile" ] && captured=true; fi; ` +
		// Try import (ImageMagick)
		`if [ "$captured" = "false" ] && command -v import >/dev/null 2>&1; then ` +
		`import -window root "$tmpfile" 2>/dev/null && [ -s "$tmpfile" ] && captured=true; fi; ` +
		// Check result
		`if [ "$captured" = "false" ]; then ` +
		`echo "no screenshot tool found (scrot, grim, gnome-screenshot, or import required)" >&2; exit 1; fi; ` +
		`base64 -w 0 < "$tmpfile" 2>/dev/null || base64 < "$tmpfile"`
}

// buildSingleMonitorScreenshotLinux returns a shell command that captures a
// single monitor by index. The approach depends on the display server:
//   - Wayland: uses `grim -o <output_name>` to capture a specific output
//   - X11: captures the full desktop then crops using python3 PIL or convert
//
// The command first tries to detect the display server and enumerate outputs,
// then captures accordingly.
func buildSingleMonitorScreenshotLinux(screenIndex int) string {
	return fmt.Sprintf(`tmpfile=$(mktemp /tmp/screenshot_XXXXXX.png); `+
		`trap "rm -f \"$tmpfile\"" EXIT; `+
		`idx=%d; `+
		// Detect display server
		`is_wayland=false; `+
		`if [ -n "$WAYLAND_DISPLAY" ]; then is_wayland=true; fi; `+
		// Wayland path: use grim -o <output_name>
		`if [ "$is_wayland" = "true" ] && command -v grim >/dev/null 2>&1; then `+
		`output_name=$(wlr-randr 2>/dev/null | grep -v "^[[:space:]]" | awk "NF{print \$1}" | sed -n "$((idx+1))p"); `+
		`if [ -z "$output_name" ]; then `+
		// Fallback: try swaymsg or wlopm to get output names
		`output_name=$(swaymsg -t get_outputs 2>/dev/null | python3 -c "`+
		`import json,sys; outputs=json.load(sys.stdin); `+
		`idx=%d; `+
		`if idx<0 or idx>=len(outputs): print('',end=''); sys.exit(1); `+
		`print(outputs[idx].get('name',''),end='')" 2>/dev/null); fi; `+
		`if [ -n "$output_name" ]; then `+
		`grim -o "$output_name" "$tmpfile" 2>/dev/null && [ -s "$tmpfile" ] && { base64 -w 0 < "$tmpfile" 2>/dev/null || base64 < "$tmpfile"; exit 0; }; fi; fi; `+
		// X11 path: capture full desktop then crop
		// First get the geometry of the target monitor via xrandr
		`if command -v xrandr >/dev/null 2>&1; then `+
		`geometry=$(xrandr --query 2>/dev/null | grep " connected" | awk "NR==$((idx+1))" | grep -oP "\d+x\d+\+\d+\+\d+"); `+
		`if [ -z "$geometry" ]; then `+
		`count=$(xrandr --query 2>/dev/null | grep -c " connected"); `+
		`echo "screen index $idx out of range: ${count} display(s) available" >&2; exit 1; fi; `+
		// Parse geometry WxH+X+Y
		`mon_w=$(echo "$geometry" | sed "s/x.*//"); `+
		`mon_h=$(echo "$geometry" | sed "s/.*x//;s/+.*//"); `+
		`mon_x=$(echo "$geometry" | sed "s/.*+//;s/+.*//" | head -c -1); `+
		// More robust parsing
		`mon_x=$(echo "$geometry" | awk -F"[x+]" "{print \$3}"); `+
		`mon_y=$(echo "$geometry" | awk -F"[x+]" "{print \$4}"); `+
		// Capture full desktop first
		`full_tmp=$(mktemp /tmp/screenshot_full_XXXXXX.png); `+
		`trap "rm -f \"$tmpfile\" \"$full_tmp\"" EXIT; `+
		`captured=false; `+
		`if command -v scrot >/dev/null 2>&1; then `+
		`scrot "$full_tmp" 2>/dev/null && [ -s "$full_tmp" ] && captured=true; fi; `+
		`if [ "$captured" = "false" ] && command -v import >/dev/null 2>&1; then `+
		`import -window root "$full_tmp" 2>/dev/null && [ -s "$full_tmp" ] && captured=true; fi; `+
		`if [ "$captured" = "false" ]; then `+
		`echo "no screenshot tool found" >&2; exit 1; fi; `+
		// Crop using python3 PIL or convert
		`if command -v python3 >/dev/null 2>&1 && python3 -c "from PIL import Image" 2>/dev/null; then `+
		`python3 -c "`+
		`from PIL import Image; import sys; `+
		`img = Image.open(sys.argv[1]); `+
		`crop = img.crop((int(sys.argv[2]), int(sys.argv[3]), int(sys.argv[2])+int(sys.argv[4]), int(sys.argv[3])+int(sys.argv[5]))); `+
		`crop.save(sys.argv[6])" "$full_tmp" "$mon_x" "$mon_y" "$mon_w" "$mon_h" "$tmpfile" 2>/dev/null; `+
		`elif command -v convert >/dev/null 2>&1; then `+
		`convert "$full_tmp" -crop "${mon_w}x${mon_h}+${mon_x}+${mon_y}" +repage "$tmpfile" 2>/dev/null; `+
		`else `+
		`cp "$full_tmp" "$tmpfile"; fi; `+
		`[ -s "$tmpfile" ] && { base64 -w 0 < "$tmpfile" 2>/dev/null || base64 < "$tmpfile"; exit 0; }; `+
		`echo "single monitor screenshot failed" >&2; exit 1; `+
		`else `+
		`echo "xrandr not available, cannot determine monitor geometry" >&2; exit 1; fi`,
		screenIndex, screenIndex)
}
