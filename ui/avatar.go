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
