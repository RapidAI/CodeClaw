//go:build darwin

package accessibility

import (
	"encoding/json"
	"testing"
)

func TestNewBridgeReturnsDarwinBridge(t *testing.T) {
	b := NewBridge()
	if _, ok := b.(*darwinBridge); !ok {
		t.Fatalf("expected *darwinBridge, got %T", b)
	}
}

func TestPyElementToElement(t *testing.T) {
	p := pyElement{
		Role:   "Button",
		Name:   "OK",
		Value:  "",
		X:      100,
		Y:      200,
		Width:  80,
		Height: 30,
		Children: []pyElement{
			{Role: "StaticText", Name: "OK", X: 105, Y: 205, Width: 70, Height: 20},
		},
	}
	el := p.toElement()
	if el.Role != "Button" {
		t.Errorf("Role = %q, want Button", el.Role)
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
	if el.Children[0].Role != "StaticText" {
		t.Errorf("Child Role = %q, want StaticText", el.Children[0].Role)
	}
}

func TestPyElementJSONRoundTrip(t *testing.T) {
	original := pyElement{
		Role:   "TextField",
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
	var decoded pyElement
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Role != original.Role || decoded.Name != original.Name || decoded.Value != original.Value {
		t.Errorf("round-trip mismatch: got %+v", decoded)
	}
}

func TestDarwinBridgeClickNilElement(t *testing.T) {
	b := &darwinBridge{}
	if err := b.ClickElement(nil); err != nil {
		t.Errorf("ClickElement(nil) returned error: %v", err)
	}
}

func TestDarwinBridgeTypeInNilElement(t *testing.T) {
	b := &darwinBridge{}
	if err := b.TypeInElement(nil, "hello"); err != nil {
		t.Errorf("TypeInElement(nil) returned error: %v", err)
	}
}

func TestDarwinBridgeGetValueNilElement(t *testing.T) {
	b := &darwinBridge{}
	val, err := b.GetValue(nil)
	if err != nil {
		t.Errorf("GetValue(nil) returned error: %v", err)
	}
	if val != "" {
		t.Errorf("GetValue(nil) = %q, want empty", val)
	}
}

func TestDarwinBridgeClose(t *testing.T) {
	b := &darwinBridge{}
	b.Close() // should not panic
}

func TestDarwinBridgeEnumNonexistentWindow(t *testing.T) {
	b := &darwinBridge{}
	elems, err := b.EnumElements("__nonexistent_window_title_12345__")
	if err != nil {
		t.Errorf("EnumElements returned error: %v", err)
	}
	if elems != nil {
		t.Errorf("expected nil for nonexistent window, got %d elements", len(elems))
	}
}

func TestDarwinBridgeFindElementNonexistentWindow(t *testing.T) {
	b := &darwinBridge{}
	el, err := b.FindElement("__nonexistent_window_title_12345__", "button", "OK")
	if err != nil {
		t.Errorf("FindElement returned error: %v", err)
	}
	if el != nil {
		t.Errorf("expected nil for nonexistent window, got %+v", el)
	}
}

func TestPyElementToElementDeepChildren(t *testing.T) {
	p := pyElement{
		Role: "Window", Name: "Main", X: 0, Y: 0, Width: 800, Height: 600,
		Children: []pyElement{
			{
				Role: "Group", Name: "Toolbar", X: 0, Y: 0, Width: 800, Height: 40,
				Children: []pyElement{
					{Role: "Button", Name: "Save", X: 10, Y: 5, Width: 60, Height: 30},
					{Role: "Button", Name: "Open", X: 80, Y: 5, Width: 60, Height: 30},
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
