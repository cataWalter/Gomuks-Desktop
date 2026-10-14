package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// TestLoginFieldsAlignAtSameWidth guards the login form layout: labels,
// explainers, entries, and the login button all stretch to the same column
// width so their left and right edges line up.
func TestLoginFieldsAlignAtSameWidth(t *testing.T) {
	testApp := test.NewApp()
	defer testApp.Quit()
	window := testApp.NewWindow("login test")
	window.Resize(fyne.NewSize(1100, 720))

	d := &DesktopApp{window: window}
	d.showLogin()

	var widths []float32
	var walk func(fyne.CanvasObject)
	walk = func(obj fyne.CanvasObject) {
		switch o := obj.(type) {
		case *widget.Entry:
			widths = append(widths, o.Size().Width)
		case *widget.Button:
			widths = append(widths, o.Size().Width)
		case *widget.Card:
			walk(o.Content)
		case *fyne.Container:
			if _, ok := o.Layout.(*fixedWidthLayout); ok && o.Size().Width != loginCardWidth {
				t.Errorf("login card width = %v, want %v", o.Size().Width, loginCardWidth)
			}
			for _, child := range o.Objects {
				walk(child)
			}
		}
	}
	walk(window.Content())

	if len(widths) != 4 {
		t.Fatalf("found %d stretchy login controls (3 entries + button), want 4", len(widths))
	}
	for i, w := range widths {
		if w != widths[0] {
			t.Errorf("login control %d width = %v, want %v so all fields align", i, w, widths[0])
		}
	}
	if widths[0] == 0 {
		t.Fatal("login controls have zero width; layout did not run")
	}
}
