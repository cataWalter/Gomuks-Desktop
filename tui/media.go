package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mychat/matrix"
)

const (
	mediaMaxRows      = 30
	mediaMinCols      = 8
	mediaMaxCols      = 120
	mediaSourceWidth  = 640
	mediaSourceHeight = 480
	mediaIndent       = "      "
)

var mediaExtensions = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/gif":       ".gif",
	"image/webp":      ".webp",
	"audio/aac":       ".aac",
	"audio/flac":      ".flac",
	"audio/mpeg":      ".mp3",
	"audio/mp4":       ".m4a",
	"audio/ogg":       ".ogg",
	"audio/opus":      ".opus",
	"audio/wav":       ".wav",
	"audio/wave":      ".wav",
	"audio/x-wav":     ".wav",
	"audio/webm":      ".webm",
	"video/mp4":       ".mp4",
	"video/quicktime": ".mov",
	"video/webm":      ".webm",
}

// mediaState is the decoded image and its rows for one URI; a missing entry means it is not requested yet.
type mediaState struct {
	src    *image.RGBA
	rows   []string
	cols   int
	failed bool
}

type posterLoadedMsg struct {
	uri string
	src *image.RGBA
	err error
}

// posterURI returns the image drawn for a message: the image itself, or a video's thumbnail.
func posterURI(msg matrix.Message) string {
	switch {
	case msg.MediaType == "m.video":
		return msg.MediaThumbnailURI
	case msg.MediaType == "m.image" || (msg.MediaType == "" && strings.HasPrefix(msg.MediaMIME, "image/")):
		return msg.MediaURI
	}
	return ""
}

func isAudioMessage(msg matrix.Message) bool {
	return msg.MediaType == "m.audio" || strings.HasPrefix(msg.MediaMIME, "audio/")
}

func audioLabel(msg matrix.Message) string {
	label := "audio message"
	if msg.MediaDurationMs > 0 {
		seconds := msg.MediaDurationMs / 1000
		label += fmt.Sprintf("  %d:%02d", seconds/60, seconds%60)
	}
	return label + "  press p to play"
}

// mediaLines returns the poster and label rows drawn below a message, cols wide.
func (m *timelineModel) mediaLines(msg matrix.Message, cols int) []string {
	var lines []string
	if uri := posterURI(msg); uri != "" {
		state, requested := m.media[uri]
		switch {
		case state.src != nil:
			for _, row := range m.posterRows(uri, cols) {
				lines = append(lines, mediaIndent+row)
			}
		case requested && state.failed:
			lines = append(lines, mediaIndent+"[image unavailable]")
		default:
			lines = append(lines, mediaIndent+"[loading media]")
		}
	}
	if msg.MediaType == "m.video" {
		lines = append(lines, mediaIndent+videoLabel(msg))
	}
	if isAudioMessage(msg) && msg.MediaURI != "" {
		lines = append(lines, mediaIndent+audioLabel(msg))
	}
	return lines
}

// posterRows renders the decoded image at cols width, reusing the rows while the width is unchanged.
func (m *timelineModel) posterRows(uri string, cols int) []string {
	state := m.media[uri]
	if state.cols != cols {
		state.rows = renderPoster(state.src, cols, mediaMaxRows)
		state.cols = cols
		m.media[uri] = state
	}
	return state.rows
}

func (m timelineModel) posterCols() int {
	return clampInt(m.viewport.Width-len(mediaIndent), mediaMinCols, mediaMaxCols)
}

func videoLabel(msg matrix.Message) string {
	label := "▶ video"
	if msg.MediaDurationMs > 0 {
		seconds := msg.MediaDurationMs / 1000
		label += fmt.Sprintf("  %d:%02d", seconds/60, seconds%60)
	}
	return label + "  press o to open"
}

func (m *timelineModel) storePoster(msg posterLoadedMsg) {
	if m.media == nil {
		m.media = make(map[string]mediaState)
	}
	m.media[msg.uri] = mediaState{src: msg.src, failed: msg.err != nil}
	m.viewport.SetContent(m.renderMessages())
}

// mediaCmd starts downloads for timeline posters that have not been requested yet.
func (m *Model) mediaCmd() tea.Cmd {
	if m.client == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, msg := range m.timeline.messages {
		uri := posterURI(msg)
		if uri == "" {
			continue
		}
		if _, requested := m.timeline.media[uri]; requested {
			continue
		}
		if m.timeline.media == nil {
			m.timeline.media = make(map[string]mediaState)
		}
		m.timeline.media[uri] = mediaState{}
		cmds = append(cmds, loadPosterCmd(m.client, uri))
	}
	return tea.Batch(cmds...)
}

func loadPosterCmd(client *matrix.Client, uri string) tea.Cmd {
	return func() tea.Msg {
		data, err := client.CachedMediaBytes(context.Background(), uri)
		if err != nil {
			return posterLoadedMsg{uri: uri, err: err}
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return posterLoadedMsg{uri: uri, err: err}
		}
		return posterLoadedMsg{uri: uri, src: fitSource(img)}
	}
}

// renderPoster draws src as quadrant blocks, two pixels per cell column and row.
func renderPoster(src *image.RGBA, maxCols, maxRows int) []string {
	bounds := src.Bounds()
	if bounds.Dx() < 1 || bounds.Dy() < 1 || maxCols < 1 || maxRows < 1 {
		return nil
	}
	cols, rows := posterGrid(bounds.Dx(), bounds.Dy(), maxCols, maxRows)
	pixels := resample(src, 2*cols, 2*rows)
	lines := make([]string, 0, rows)
	for cy := range rows {
		var line strings.Builder
		for cx := range cols {
			var cell [4]rgb
			for i := range cell {
				cell[i] = rgbAt(pixels, 2*cx+i%2, 2*cy+i/2)
			}
			mask, fg, bg := bestQuadrant(cell)
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(fg.hex())).Background(lipgloss.Color(bg.hex()))
			line.WriteString(style.Render(quadrantGlyphs[mask]))
		}
		lines = append(lines, line.String())
	}
	return lines
}

// quadrantGlyphs indexes block characters by filled quadrant: top-left 1, top-right 2, bottom-left 4, bottom-right 8.
var quadrantGlyphs = [16]string{" ", "▘", "▝", "▀", "▖", "▌", "▞", "▛", "▗", "▚", "▐", "▜", "▄", "▙", "▟", "█"}

// posterGrid sizes the poster in cells; a terminal cell is about twice as tall as it is wide.
func posterGrid(srcWidth, srcHeight, maxCols, maxRows int) (int, int) {
	aspect := float64(srcWidth) / float64(srcHeight)
	cols := min(maxCols, int(math.Round(2*aspect*float64(maxRows))))
	rows := min(maxRows, int(math.Round(float64(cols)/(2*aspect))))
	cols = min(cols, max(1, srcWidth/2))
	rows = min(rows, max(1, srcHeight/2))
	return max(1, cols), max(1, rows)
}

func fitSource(img image.Image) *image.RGBA {
	bounds := img.Bounds()
	scale := min(1.0, float64(mediaSourceWidth)/float64(bounds.Dx()), float64(mediaSourceHeight)/float64(bounds.Dy()))
	return resample(img, max(1, int(float64(bounds.Dx())*scale)), max(1, int(float64(bounds.Dy())*scale)))
}

// resample averages the source pixels that map to each target pixel.
func resample(src image.Image, width, height int) *image.RGBA {
	bounds := src.Bounds()
	srcWidth, srcHeight := bounds.Dx(), bounds.Dy()
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		y0 := y * srcHeight / height
		y1 := max((y+1)*srcHeight/height, y0+1)
		for x := range width {
			x0 := x * srcWidth / width
			x1 := max((x+1)*srcWidth/width, x0+1)
			var r, g, b, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, _ := src.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
					r, g, b, n = r+uint64(cr), g+uint64(cg), b+uint64(cb), n+1
				}
			}
			out.SetRGBA(x, y, color.RGBA{R: uint8((r / n) >> 8), G: uint8((g / n) >> 8), B: uint8((b / n) >> 8), A: 255})
		}
	}
	return out
}

type rgb [3]int

func rgbAt(img *image.RGBA, x, y int) rgb {
	c := img.RGBAAt(x, y)
	return rgb{int(c.R), int(c.G), int(c.B)}
}

func (c rgb) plus(o rgb) rgb { return rgb{c[0] + o[0], c[1] + o[1], c[2] + o[2]} }

func (c rgb) over(n int) rgb { return rgb{c[0] / n, c[1] / n, c[2] / n} }

func (c rgb) squaredDistance(o rgb) int {
	sum := 0
	for i := range c {
		d := c[i] - o[i]
		sum += d * d
	}
	return sum
}

func (c rgb) hex() string { return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]) }

// bestQuadrant returns the glyph mask and the two colours that reproduce the four pixels with least error.
func bestQuadrant(cell [4]rgb) (int, rgb, rgb) {
	bestMask, bestErr := 0, -1
	var bestFG, bestBG rgb
	for mask := range 16 {
		var sum [2]rgb
		var count [2]int
		for i, px := range cell {
			group := (mask >> i) & 1
			sum[group] = sum[group].plus(px)
			count[group]++
		}
		var mean [2]rgb
		for g := range mean {
			if count[g] > 0 {
				mean[g] = sum[g].over(count[g])
			}
		}
		if count[0] == 0 {
			mean[0] = mean[1]
		}
		if count[1] == 0 {
			mean[1] = mean[0]
		}
		total := 0
		for i, px := range cell {
			total += px.squaredDistance(mean[(mask>>i)&1])
		}
		if bestErr < 0 || total < bestErr {
			bestMask, bestErr, bestFG, bestBG = mask, total, mean[1], mean[0]
		}
	}
	return bestMask, bestFG, bestBG
}

// openMediaCmd saves the attachment as a file with its extension and opens it with the system viewer.
func openMediaCmd(client *matrix.Client, msg matrix.Message) tea.Cmd {
	return func() tea.Msg {
		data, err := client.CachedMediaBytes(context.Background(), msg.MediaURI)
		if err != nil {
			return ActionResultMsg{Err: err}
		}
		path, err := writeOpenableMedia(client.MediaCacheDir(), msg, data)
		if err != nil {
			return ActionResultMsg{Err: err}
		}
		return ActionResultMsg{Err: launchFile(path)}
	}
}

func writeOpenableMedia(cacheDir string, msg matrix.Message, data []byte) (string, error) {
	if cacheDir == "" {
		return "", errors.New("media cache directory is not set")
	}
	dir := filepath.Join(cacheDir, "open")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create media folder: %w", err)
	}
	mimeType := msg.MediaMIME
	if mimeType == "" || mimeType == "image/unknown" {
		mimeType = http.DetectContentType(data)
	}
	path := filepath.Join(dir, safeFileStem(msg.ID)+mediaExtensions[mimeType])
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("save media: %w", err)
	}
	return path, nil
}

func safeFileStem(id string) string {
	stem := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return '_'
	}, id)
	if stem == "" {
		return "media"
	}
	return stem
}

func launchFile(path string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	cmd := exec.Command(opener, path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open media: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
