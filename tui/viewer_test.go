package tui

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestItermInlineSequenceCarriesSizeAndPayload(t *testing.T) {
	want := "\x1b]1337;File=inline=1;width=80;height=20;preserveAspectRatio=1:QUJD\a"
	if got := itermInline("QUJD", 80, 20); got != want {
		t.Fatalf("sequence = %q, want %q", got, want)
	}
}

func TestViewKeyNeedsNativeImages(t *testing.T) {
	model := mediaTestModel()
	model.setFocus(FocusTimeline)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	got := updated.(Model)
	if got.viewer != nil || !strings.Contains(got.errorMsg, "GOMUKS_TUI_IMAGES") {
		t.Fatalf("viewer = %v, error = %q; want native-image hint", got.viewer, got.errorMsg)
	}
}

func TestViewerPlacesImageAndClosesOnEscape(t *testing.T) {
	model := mediaTestModel()
	model.nativeImages = true
	model.viewer = &imageViewer{label: "test", payload: "QUJD"}
	view := model.View()
	if !strings.Contains(view, "\x1b7\x1b[2;1H\x1b]1337;File=inline=1;width=100;height=28") {
		t.Fatal("viewer does not place the image below the header")
	}
	lines := strings.Split(view, "\n")
	if len(lines) != 30 {
		t.Fatalf("viewer rows = %d, want the terminal height 30", len(lines))
	}
	if width := lipgloss.Width(lines[len(lines)-1]); width != 99 {
		t.Fatalf("footer width = %d, want 99 without the image sequence", width)
	}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).viewer != nil || cmd == nil {
		t.Fatal("escape did not close the viewer and clear the screen")
	}
}

func TestEncodeScaledPNGLimitsLongSide(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, gradientImage(3000, 10)); err != nil {
		t.Fatal(err)
	}
	data, err := encodeScaledPNG(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if width := decoded.Bounds().Dx(); width > viewerMaxSide {
		t.Fatalf("width = %d, want at most %d", width, viewerMaxSide)
	}
}
