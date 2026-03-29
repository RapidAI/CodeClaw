//go:build linux

package accessibility

import (
	"encoding/json"
	"testing"
)

func TestNewBridgeReturnsLinuxBridge(t *testing.T) {
	b := NewBridge()
	if _, ok := b.(*linuxBridge); !ok {
		t.Fatalf("expected *linuxBridge, got %T", b)
	}
}

func TestAtspiElementToElement(t *testing.T) {
	p := atspiElement{
		Role:   "push button",
		Name:   "OK",
		Value:  "",
		X:      100,
		Y:      200,
		Width:  80,
		Height: 30,
		Children: []atspiElement{
			{Role: "label", Name: "OK", X: 105, Y: 205, Width: 70, Height: 20},
		},
	}
	el := p.toElement()
	if el.Role != "push button" {
		t.Errorf("Role = %q, want 'push button'", el.Role)
	}
	if el.Name != "OK" {
		t.Errorf("Name = %q, want OK", el.Name)
	}
	if el.Bounds.X != 100 || el.Bounds.Y != 200 || el.Bounds.Width != 80 || el.Bounds.Height != 30 {
		t.Errorf("Bounds = %+v, want {100 200 80 30}", el.Bounds)
	}
	if len(el.Children) != 1 {
		t.Fatalf("Children count = %d, want 1", len(el.Children))
	}
	if el.Children[0].Role != "label" {
		t.Errorf("Child Role = %q, want label", el.Children[0].Role)
	}
}

func TestAtspiElementJSONRoundTrip(t *testing.T) {
	original := atspiElement{
		Role:   "text",
		Name:   "Username",
		Value:  "admin",
		X:      50,
		Y:      100,
		Width:  200,
		Height: 25,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded atspiElement
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Role != original.Role || decoded.Name != original.Name || decoded.Value != original.Value {
		t.Errorf("round-trip mismatch: got %+v", decoded)
	}
	if decoded.X != original.X || decoded.Y != original.Y || decoded.Width != original.Width || decoded.Height != original.Height {
		t.Errorf("round-trip bounds mismatch: got %+v", decoded)
	}
}

func TestLinuxBridgeClickNilElement(t *testing.T) {
	b := &linuxBridge{}
	if err := b.ClickElement(nil); err != nil {
		t.Errorf("ClickElement(nil) returned error: %v", err)
	}
}

func TestLinuxBridgeTypeInNilElement(t *testing.T) {
	b := &linuxBridge{}
	if err := b.TypeInElement(nil, "hello"); err != nil {
		t.Errorf("TypeInElement(nil) returned error: %v", err)
	}
}

func TestLinuxBridgeGetValueNilElement(t *testing.T) {
	b := &linuxBridge{}
	val, err := b.GetValue(nil)
	if err != nil {
		t.Errorf("GetValue(nil) returned error: %v", err)
	}
	if val != "" {
		t.Errorf("GetValue(nil) = %q, want empty", val)
	}
}

func TestLinuxBridgeClose(t *testing.T) {
	b := &linuxBridge{}
	b.Close() // should not panic
}

func TestLinuxBridgeEnumNonexistentWindow(t *testing.T) {
	b := &linuxBridge{}
	elems, err := b.EnumElements("__nonexistent_window_title_12345__")
	if err != nil {
		t.Errorf("EnumElements returned error: %v", err)
	}
	if elems != nil {
		t.Errorf("expected nil for nonexistent window, got %d elements", len(elems))
	}
}

func TestLinuxBridgeFindElementNonexistentWindow(t *testing.T) {
	b := &linuxBridge{}
	el, err := b.FindElement("__nonexistent_window_title_12345__", "push button", "OK")
	if err != nil {
		t.Errorf("FindElement returned error: %v", err)
	}
	if el != nil {
		t.Errorf("expected nil for nonexistent window, got %+v", el)
	}
}

func TestAtspiElementToElementDeepChildren(t *testing.T) {
	p := atspiElement{
		Role: "frame", Name: "Main", X: 0, Y: 0, Width: 800, Height: 600,
		Children: []atspiElement{
			{
				Role: "panel", Name: "Toolbar", X: 0, Y: 0, Width: 800, Height: 40,
				Children: []atspiElement{
					{Role: "push button", Name: "Save", X: 10, Y: 5, Width: 60, Height: 30},
					{Role: "push button", Name: "Open", X: 80, Y: 5, Width: 60, Height: 30},
				},
			},
		},
	}
	el := p.toElement()
	if len(el.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(el.Children))
	}
	toolbar := el.Children[0]
	if len(toolbar.Children) != 2 {
		t.Fatalf("expected 2 toolbar children, got %d", len(toolbar.Children))
	}
	if toolbar.Children[0].Name != "Save" || toolbar.Children[1].Name != "Open" {
		t.Errorf("unexpected toolbar children: %+v", toolbar.Children)
	}
}
