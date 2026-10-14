package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"strings"
	"sync"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type avatarFetcher func(context.Context, string) ([]byte, error)
type mediaFetcher func(context.Context, string) ([]byte, error)

type imageResult struct {
	image        image.Image
	loading      bool
	waiters      []func(image.Image)
	errorWaiters []func(error)
}

// mediaCache is a small in-memory image cache for inline media thumbnails and
// the fullscreen lightbox. The matrix client owns the encrypted disk cache;
// this layer only avoids decoding the same bytes repeatedly.
type mediaCache struct {
	ctx   context.Context
	fetch mediaFetcher
	mu    sync.Mutex
	items map[string]*imageResult
}

func newMediaCache(ctx context.Context, fetch mediaFetcher) *mediaCache {
	return &mediaCache{ctx: ctx, fetch: fetch, items: make(map[string]*imageResult)}
}

func (c *mediaCache) load(key string, callback func(image.Image)) {
	c.loadWithError(key, callback, nil)
}

// loadWithError fetches and decodes the image for key, invoking callback with
// the result or onError when the download or decode fails. Failed entries are
// evicted so a later request can retry instead of caching the failure forever.
func (c *mediaCache) loadWithError(key string, callback func(image.Image), onError func(error)) {
	if key == "" || c == nil || c.fetch == nil {
		return
	}
	c.mu.Lock()
	if result, ok := c.items[key]; ok {
		if result.loading {
			result.waiters = append(result.waiters, callback)
			result.errorWaiters = append(result.errorWaiters, onError)
			c.mu.Unlock()
			return
		}
		img := result.image
		c.mu.Unlock()
		if img != nil {
			fyne.Do(func() { callback(img) })
		}
		return
	}
	result := &imageResult{
		loading:      true,
		waiters:      []func(image.Image){callback},
		errorWaiters: []func(error){onError},
	}
	c.items[key] = result
	c.mu.Unlock()

	go func() {
		data, err := c.fetch(c.ctx, key)
		var img image.Image
		if err == nil {
			img, _, err = image.Decode(bytes.NewReader(data))
		}
		if err != nil {
			log.Printf("decode Matrix media image: %v", err)
		}
		c.mu.Lock()
		result.loading = false
		result.image = img
		if err != nil {
			// Drop the failed entry so a future request retries the fetch.
			delete(c.items, key)
		}
		waiters := result.waiters
		errorWaiters := result.errorWaiters
		result.waiters = nil
		result.errorWaiters = nil
		c.mu.Unlock()
		if err != nil {
			fyne.Do(func() {
				for _, waiter := range errorWaiters {
					if waiter != nil {
						waiter(err)
					}
				}
			})
			return
		}
		fyne.Do(func() {
			for _, waiter := range waiters {
				waiter(img)
			}
		})
	}()
}

type avatarResult struct {
	image   image.Image
	loading bool
	waiters []func(image.Image)
}

type avatarCache struct {
	ctx     context.Context
	fetch   avatarFetcher
	mu      sync.Mutex
	avatars map[string]*avatarResult
}

func newAvatarCache(ctx context.Context, fetch avatarFetcher) *avatarCache {
	return &avatarCache{ctx: ctx, fetch: fetch, avatars: make(map[string]*avatarResult)}
}

func (c *avatarCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	clear(c.avatars)
	c.mu.Unlock()
}

func (c *avatarCache) Count() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.avatars)
}

func (c *avatarCache) load(uri string, callback func(image.Image)) {
	if uri == "" || c == nil || c.fetch == nil {
		return
	}
	c.mu.Lock()
	if result, ok := c.avatars[uri]; ok {
		if result.loading {
			result.waiters = append(result.waiters, callback)
			c.mu.Unlock()
			return
		}
		img := result.image
		c.mu.Unlock()
		if img != nil {
			fyne.Do(func() { callback(img) })
		}
		return
	}
	result := &avatarResult{loading: true, waiters: []func(image.Image){callback}}
	c.avatars[uri] = result
	c.mu.Unlock()

	go func() {
		data, err := c.fetch(c.ctx, uri)
		var img image.Image
		if err == nil {
			img, err = decodeAvatar(data)
		}
		if err != nil {
			log.Printf("load Matrix profile image: %v", err)
		}
		c.mu.Lock()
		result.loading = false
		result.image = img
		waiters := result.waiters
		result.waiters = nil
		c.mu.Unlock()
		if img != nil {
			fyne.Do(func() {
				for _, waiter := range waiters {
					waiter(img)
				}
			})
		}
	}()
}

func decodeAvatar(data []byte) (image.Image, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode avatar image header: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 2048 || config.Height > 2048 {
		return nil, fmt.Errorf("avatar image dimensions %dx%d are outside the supported range", config.Width, config.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode avatar image: %w", err)
	}
	return resizeAvatar(img, 96), nil
}

func resizeAvatar(src image.Image, maxDimension int) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= maxDimension && height <= maxDimension {
		return src
	}
	scale := float64(maxDimension) / float64(max(width, height))
	dstWidth := max(1, int(float64(width)*scale))
	dstHeight := max(1, int(float64(height)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	for y := 0; y < dstHeight; y++ {
		for x := 0; x < dstWidth; x++ {
			srcX := bounds.Min.X + x*width/dstWidth
			srcY := bounds.Min.Y + y*height/dstHeight
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

type avatarView struct {
	root        *fyne.Container
	placeholder *fyne.Container
	image       *canvas.Image
	size        float32
	uri         string
	name        string
	cache       *avatarCache
}

func newAvatarView(size float32) *avatarView {
	circle := canvas.NewCircle(theme.Color(theme.ColorNamePrimary))
	initials := widget.NewLabel("")
	initials.Alignment = fyne.TextAlignCenter
	initials.TextStyle.Bold = true
	placeholder := container.NewStack(circle, container.NewCenter(initials))
	img := canvas.NewImageFromImage(nil)
	img.FillMode = canvas.ImageFillCover
	img.CornerRadius = size / 2
	img.Hide()
	return &avatarView{
		root:        container.NewGridWrap(fyne.NewSquareSize(size), container.NewStack(placeholder, img)),
		placeholder: placeholder,
		image:       img,
		size:        size,
	}
}

func (a *avatarView) set(uri, name string, cache *avatarCache) {
	if a.uri == uri && a.name == name && a.cache == cache {
		return
	}
	if a.uri == uri && a.cache == cache {
		a.name = name
		if uri == "" {
			a.setInitials(name)
		}
		return
	}
	a.uri = uri
	a.name = name
	a.cache = cache
	a.image.Hide()
	a.placeholder.Show()
	a.setInitials(name)
	if uri == "" || cache == nil {
		return
	}
	cache.load(uri, func(img image.Image) {
		if a.uri != uri {
			return
		}
		a.image.Image = img
		a.image.Resize(fyne.NewSquareSize(a.size))
		a.placeholder.Hide()
		a.image.Show()
		a.image.Refresh()
	})
}

func (a *avatarView) setInitials(name string) {
	name = strings.TrimSpace(strings.TrimPrefix(name, "@"))
	name = strings.SplitN(name, ":", 2)[0]
	words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	initials := "?"
	if len(words) > 0 {
		initials = firstRune(words[0])
		if len(words) > 1 {
			initials += firstRune(words[1])
		}
	}
	hash := sha256.Sum256([]byte(name))
	circle := a.placeholder.Objects[0].(*canvas.Circle)
	circle.FillColor = colorFromHash(hash)
	circle.Refresh()
	label := a.placeholder.Objects[1].(*fyne.Container).Objects[0].(*widget.Label)
	label.SetText(strings.ToUpper(initials))
}

func firstRune(value string) string {
	for _, r := range value {
		return string(r)
	}
	return "?"
}

func colorFromHash(hash [32]byte) color.Color {
	return color.NRGBA{
		R: 70 + hash[0]%110,
		G: 70 + hash[1]%110,
		B: 70 + hash[2]%110,
		A: 255,
	}
}

// Inline timeline thumbnails are capped at this box; the real image aspect
// ratio is preserved inside it when the homeserver reports dimensions.
const (
	thumbMaxWidth  = 320
	thumbMaxHeight = 240
)

// thumbDisplaySize computes the displayed size of an inline image thumbnail,
// preserving aspect ratio without upscaling past the original pixel size.
// Unknown dimensions fall back to the full thumbnail box.
func thumbDisplaySize(width, height int) fyne.Size {
	if width <= 0 || height <= 0 {
		return fyne.NewSize(thumbMaxWidth, thumbMaxHeight)
	}
	scale := float64(thumbMaxWidth) / float64(width)
	if heightScale := float64(thumbMaxHeight) / float64(height); heightScale < scale {
		scale = heightScale
	}
	if scale > 1 {
		scale = 1
	}
	return fyne.NewSize(
		float32(int(float64(width)*scale)),
		float32(int(float64(height)*scale)),
	)
}

// tappableImage is a canvas.Image that invokes a callback when tapped. It is
// used for inline image thumbnails that open the fullscreen media viewer.
type tappableImage struct {
	canvas.Image
	tapped func()
}

func (t *tappableImage) Tapped(*fyne.PointEvent) {
	if t.tapped != nil {
		t.tapped()
	}
}

func (t *tappableImage) Tappable() bool { return t.tapped != nil }

// setImage assigns the decoded image to the embedded canvas.Image. The
// embedded field itself is named Image, so the data field needs explicit
// access through the embedded struct.
func (t *tappableImage) setImage(img image.Image) {
	t.Image.Image = img
	t.Refresh()
}

func (t *tappableImage) hasImage() bool { return t.Image.Image != nil }

// mediaThumbView renders an inline timeline image thumbnail with a loading
// placeholder and rounded corners. Rows are recycled by widget.List, so the
// recorded uri guards against stale async image results.
type mediaThumbView struct {
	root        *fyne.Container
	placeholder *fyne.Container
	phLabel     *widget.Label
	image       *tappableImage
	size        fyne.Size
	uri         string
}

func newMediaThumbView() *mediaThumbView {
	rect := canvas.NewRectangle(theme.Color(theme.ColorNameDisabled))
	rect.SetMinSize(fyne.NewSize(thumbMaxWidth, thumbMaxHeight))
	phLabel := widget.NewLabel("Loading image…")
	phLabel.Alignment = fyne.TextAlignCenter
	placeholder := container.NewStack(rect, container.NewCenter(phLabel))
	img := &tappableImage{}
	img.FillMode = canvas.ImageFillContain
	img.CornerRadius = theme.Padding() * 2
	img.Hide()
	thumb := &mediaThumbView{
		placeholder: placeholder,
		phLabel:     phLabel,
		image:       img,
	}
	// HBox keeps the thumbnail at its natural size instead of stretching it
	// to the full message column width.
	thumb.root = container.NewHBox(container.NewStack(placeholder, img))
	thumb.root.Hide()
	return thumb
}

// set shows the thumbnail for an image message, loading the bytes through the
// media cache. open is invoked when the user taps the loaded image.
func (m *mediaThumbView) set(uri, roomID string, width, height int, cache *mediaCache, open func()) {
	m.image.tapped = nil
	if uri == "" {
		m.uri = ""
		m.root.Hide()
		return
	}
	if size := thumbDisplaySize(width, height); m.size != size {
		m.size = size
		m.image.SetMinSize(size)
		if rect, ok := m.placeholder.Objects[0].(*canvas.Rectangle); ok {
			rect.SetMinSize(size)
		}
	}
	if open != nil {
		m.image.tapped = func() { open() }
	}
	m.root.Show()
	if m.uri == uri && m.image.hasImage() {
		return // Already loaded for this message.
	}
	m.uri = uri
	m.image.Hide()
	m.phLabel.SetText("Loading image…")
	m.placeholder.Show()
	if cache == nil {
		m.phLabel.SetText("Inline preview unavailable")
		return
	}
	key := uri + "\x00" + roomID
	cache.loadWithError(key, func(img image.Image) {
		if m.uri != uri {
			return // Row was recycled for a different message.
		}
		m.image.setImage(img)
		m.placeholder.Hide()
		m.image.Show()
		m.image.Refresh()
	}, func(error) {
		if m.uri != uri {
			return
		}
		m.uri = "" // Allow a retry on the next timeline refresh.
		m.phLabel.SetText("Unable to load image")
	})
}

// hide collapses the thumbnail for non-image messages.
func (m *mediaThumbView) hide() {
	m.uri = ""
	m.image.tapped = nil
	m.root.Hide()
}
