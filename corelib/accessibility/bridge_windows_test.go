//go:build windows

package accessibility

import (
	"encoding/json"
	"testing"
)

func TestNewBridgeReturnsWindowsBridge(t *testing.T) {
	b := NewBridge()
	if _, ok := b.(*windowsBridge); !ok {
		t.Fatalf("expected *windowsBridge, got %T", b)
	}
}

func TestPsElementToElement(t *testing.T) {
	p := psElement{
		Role:   "Button",
		Name:   "OK",
		Value:  "",
		X:      100,
		Y:      200,
		Width:  80,
		Height: 30,
		Children: []psElement{
			{Role: "Text", Name: "OK", X: 105, Y: 205, Width: 70, Height: 20},
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
	if el.Children[0].Role != "Text" {
		t.Errorf("Child Role = %q, want Text", el.Children[0].Role)
	}
}

func TestPsElementJSONRoundTrip(t *testing.T) {
	original := psElement{
		Role:   "Edit",
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
	var decoded psElement
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Role != original.Role || decoded.Name != original.Name || decoded.Value != original.Value {
		t.Errorf("round-trip mismatch: got %+v", decoded)
	}
}

func TestWindowsBridgeClickNilElement(t *testing.T) {
	b := &windowsBridge{}
	if err := b.ClickElement(nil); err != nil {
		t.Errorf("ClickElement(nil) returned error: %v", err)
	}
}

func TestWindowsBridgeTypeInNilElement(t *testing.T) {
	b := &windowsBridge{}
	if err := b.TypeInElement(nil, "hello"); err != nil {
		t.Errorf("TypeInElement(nil) returned error: %v", err)
	}
}

func TestWindowsBridgeGetValueNilElement(t *testing.T) {
	b := &windowsBridge{}
	val, err := b.GetValue(nil)
	if err != nil {
		t.Errorf("GetValue(nil) returned error: %v", err)
	}
	if val != "" {
		t.Errorf("GetValue(nil) = %q, want empty", val)
	}
}

func TestWindowsBridgeClose(t *testing.T) {
	b := &windowsBridge{}
	b.Close() // should not panic
}

func TestWindowsBridgeEnumNonexistentWindow(t *testing.T) {
	b := &windowsBridge{}
	elems, err := b.EnumElements("__nonexistent_window_title_12345__")
	if err != nil {
		t.Errorf("EnumElements returned error: %v", err)
	}
	if elems != nil {
		t.Errorf("expected nil for nonexistent window, got %d elements", len(elems))
	}
}

func TestWindowsBridgeFindElementNonexistentWindow(t *testing.T) {
	b := &windowsBridge{}
	el, err := b.FindElement("__nonexistent_window_title_12345__", "button", "OK")
	if err != nil {
		t.Errorf("FindElement returned error: %v", err)
	}
	if el != nil {
		t.Errorf("expected nil for nonexistent window, got %+v", el)
	}
}
