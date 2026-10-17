package tui

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"mychat/matrix"
)

func mediaTestModel() Model {
	model := *NewModel(nil)
	model.loginActive = false
	model.timeline.setMessages([]matrix.Message{
		{ID: "$text", Body: "hello"},
		{ID: "$img", Body: "photo.png", MediaType: "m.image", MediaURI: "mxc://example.org/photo", MediaMIME: "image/png"},
	}, false)
	return model
}

func gradientImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	return img
}

func TestRenderPosterFitsCellBudget(t *testing.T) {
	lines := renderPoster(gradientImage(200, 100), 40, 14)
	if len(lines) == 0 || len(lines) > 14 {
		t.Fatalf("poster rows = %d, want between 1 and 14", len(lines))
	}
	for _, line := range lines {
		if width := lipgloss.Width(line); width > 40 {
			t.Fatalf("poster row width = %d, want at most 40", width)
		}
	}
}

func TestPosterGridKeepsAspectRatio(t *testing.T) {
	if cols, rows := posterGrid(200, 100, 40, 14); cols != 40 || rows != 10 {
		t.Fatalf("grid = %dx%d, want 40x10", cols, rows)
	}
}

func TestBestQuadrantReproducesPixels(t *testing.T) {
	cell := [4]rgb{{0, 0, 0}, {0, 0, 0}, {255, 255, 255}, {255, 255, 255}}
	mask, fg, bg := bestQuadrant(cell)
	for i, px := range cell {
		want := bg
		if (mask>>i)&1 == 1 {
			want = fg
		}
		if px != want {
			t.Fatalf("pixel %d = %v, want %v (mask %d)", i, px, want, mask)
		}
	}
}

func TestPosterURIChoosesImageOrThumbnail(t *testing.T) {
	if got := posterURI(matrix.Message{MediaType: "m.image", MediaURI: "mxc://a/img"}); got != "mxc://a/img" {
		t.Fatalf("image poster = %q, want the image", got)
	}
	if got := posterURI(matrix.Message{MediaType: "m.video", MediaURI: "mxc://a/clip", MediaThumbnailURI: "mxc://a/thumb"}); got != "mxc://a/thumb" {
		t.Fatalf("video poster = %q, want the thumbnail", got)
	}
	if got := posterURI(matrix.Message{Body: "plain text"}); got != "" {
		t.Fatalf("text poster = %q, want none", got)
	}
}

func TestPosterLoadedRendersUnderImageMessage(t *testing.T) {
	model := mediaTestModel()
	updated, _ := model.Update(posterLoadedMsg{uri: "mxc://example.org/photo", src: gradientImage(200, 100)})
	model = updated.(Model)
	rows := strings.Split(model.timeline.renderMessages(), "\n")
	owned := 0
	for i := range rows {
		if owner, ok := model.timeline.messageAtLine(i); ok && owner == 1 {
			owned++
		}
	}
	if owned < 2 {
		t.Fatalf("message 1 owns %d rows, want its header and poster", owned)
	}
}

func TestFailedPosterShowsPlaceholder(t *testing.T) {
	model := mediaTestModel()
	updated, _ := model.Update(posterLoadedMsg{uri: "mxc://example.org/photo", err: errors.New("download failed")})
	model = updated.(Model)
	if rendered := model.timeline.renderMessages(); !strings.Contains(rendered, "[image unavailable]") {
		t.Fatalf("failed poster placeholder missing:\n%s", rendered)
	}
}

func TestVideoRowShowsDurationAndOpenHint(t *testing.T) {
	model := *NewModel(nil)
	model.loginActive = false
	model.timeline.setMessages([]matrix.Message{{
		ID:                "$clip",
		Body:              "clip.mp4",
		MediaType:         "m.video",
		MediaURI:          "mxc://example.org/clip",
		MediaThumbnailURI: "mxc://example.org/thumb",
		MediaDurationMs:   65000,
	}}, false)
	rendered := model.timeline.renderMessages()
	if !strings.Contains(rendered, "[loading media]") {
		t.Fatalf("video poster placeholder missing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "▶ video  1:05  press o to open") {
		t.Fatalf("video label missing:\n%s", rendered)
	}
}

func TestVoiceWAVHasPCMHeadersAndDuration(t *testing.T) {
	pcm := []byte{0x34, 0x12, 0x78, 0x56}
	wav, err := encodeVoiceWAV(pcm, 16000)
	if err != nil {
		t.Fatal(err)
	}
	if string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatalf("invalid WAV signature: %q %q", wav[:4], wav[8:12])
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != 16000 {
		t.Fatalf("sample rate = %d, want 16000", got)
	}
	if got := binary.LittleEndian.Uint32(wav[40:44]); got != uint32(len(pcm)) {
		t.Fatalf("PCM data size = %d, want %d", got, len(pcm))
	}
}

func TestAudioMessageLabelShowsDurationAndPlayKey(t *testing.T) {
	label := audioLabel(matrix.Message{MediaType: "m.audio", MediaDurationMs: 6500})
	if !strings.Contains(label, "0:06") || !strings.Contains(label, "press p to play") {
		t.Fatalf("audio label = %q, want duration and play hint", label)
	}
}

func TestWriteOpenableAudioUsesPlayableExtension(t *testing.T) {
	path, err := writeOpenableMedia(t.TempDir(), matrix.Message{ID: "$voice", MediaMIME: "audio/wave"}, []byte("audio"))
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Ext(path); got != ".wav" {
		t.Fatalf("audio file extension = %q, want .wav", got)
	}
}
