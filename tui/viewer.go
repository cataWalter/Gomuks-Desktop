package tui

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mychat/matrix"
)

const (
	viewerMaxBytes = 6 << 20
	viewerMaxSide  = 2048
)

// imageViewer is the full-screen native image opened with the v key.
type imageViewer struct {
	uri     string
	label   string
	payload string
}

type viewerLoadedMsg struct {
	uri     string
	label   string
	payload string
	err     error
}

func loadViewerCmd(client *matrix.Client, msg matrix.Message) tea.Cmd {
	uri := posterURI(msg)
	label := viewerLabel(msg)
	return func() tea.Msg {
		data, err := client.CachedMediaBytes(context.Background(), uri)
		if err != nil {
			return viewerLoadedMsg{uri: uri, err: err}
		}
		if len(data) > viewerMaxBytes {
			if data, err = encodeScaledPNG(data); err != nil {
				return viewerLoadedMsg{uri: uri, err: err}
			}
		}
		return viewerLoadedMsg{uri: uri, label: label, payload: base64.StdEncoding.EncodeToString(data)}
	}
}

func encodeScaledPNG(data []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	scale := min(1.0, float64(viewerMaxSide)/float64(max(bounds.Dx(), bounds.Dy())))
	scaled := resample(img, max(1, int(float64(bounds.Dx())*scale)), max(1, int(float64(bounds.Dy())*scale)))
	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func viewerLabel(msg matrix.Message) string {
	sender := msg.SenderName
	if sender == "" {
		sender = msg.Sender
	}
	return fmt.Sprintf("%s · %s", sender, msg.Timestamp.Format("2006-01-02 15:04"))
}

func itermInline(payload string, cols, rows int) string {
	return fmt.Sprintf("\x1b]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=1:%s\a", cols, rows, payload)
}

// placeAt draws sequence at a 1-based screen position and restores the cursor.
func placeAt(row, col int, sequence string) string {
	return fmt.Sprintf("\x1b7\x1b[%d;%dH%s\x1b8", row, col, sequence)
}

func (m Model) viewerView() string {
	width, height := m.size()
	rows := max(height-2, 1)
	lines := make([]string, 0, rows+2)
	lines = append(lines, headerStyle.Width(width).Render(fitLine(m.viewer.label, width-2)))
	for i := range rows {
		line := ""
		if i == 0 && m.viewer.payload == "" {
			line = "loading image…"
		}
		lines = append(lines, line)
	}
	footer := footerStyle.Width(width - 1).Render(fitLine("esc closes the image", width-3))
	if m.viewer.payload != "" {
		footer += placeAt(2, 1, itermInline(m.viewer.payload, width, rows))
	}
	return strings.Join(append(lines, footer), "\n")
}

func (m Model) handleViewerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "v":
		m.viewer = nil
		return m, tea.ClearScreen
	}
	return m, nil
}
