package ui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"mychat/matrix"
)

func encodedTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var encoded bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.White)
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatalf("encode test image: %v", err)
	}
	return encoded.Bytes()
}

// waitForCondition polls cond for up to two seconds. The test driver runs
// fyne.Do callbacks inline on the loading goroutine, so async cache results
// settle without an event loop.
func waitForCondition(cond func() bool) bool {
	for i := 0; i < 400; i++ {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func TestThumbDisplaySizePreservesAspectRatio(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		want          fyne.Size
	}{
		{name: "landscape fits width", width: 640, height: 480, want: fyne.NewSize(320, 240)},
		{name: "portrait fits height", width: 480, height: 640, want: fyne.NewSize(180, 240)},
		{name: "small images are not upscaled", width: 100, height: 50, want: fyne.NewSize(100, 50)},
		{name: "unknown dimensions use full box", width: 0, height: 0, want: fyne.NewSize(320, 240)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := thumbDisplaySize(test.width, test.height); got != test.want {
				t.Fatalf("thumbDisplaySize(%d, %d) = %v, want %v", test.width, test.height, got, test.want)
			}
		})
	}
}

func TestChatViewShowsInlineImageThumbnail(t *testing.T) {
	testApp := test.NewApp()
	defer testApp.Quit()
	window := testApp.NewWindow("chat media test")
	view := NewChatView(nil)
	view.SetRoom(matrix.Room{ID: "!room:example.org", Name: "Room"})
	window.SetContent(view.Object())
	window.Resize(fyne.NewSize(900, 700))

	view.SetMessages([]matrix.Message{{
		ID:          "$image",
		Sender:      "@alice:example.org",
		SenderName:  "Alice",
		Body:        "cat.png",
		Timestamp:   time.Now(),
		MediaURI:    "mxc://example.org/cat",
		MediaMIME:   "image/png",
		MediaWidth:  640,
		MediaHeight: 480,
	}})
	view.messages.Refresh()

	var thumb *mediaThumbView
	for _, candidate := range view.mediaThumbs {
		thumb = candidate
		break
	}
	if thumb == nil {
		t.Fatal("message list created no thumbnail views")
	}
	if !thumb.root.Visible() {
		t.Fatal("image message should show an inline thumbnail")
	}
	if want := fyne.NewSize(320, 240); thumb.size != want {
		t.Fatalf("thumbnail size = %v, want %v", thumb.size, want)
	}

	view.SetMessages([]matrix.Message{{
		ID:        "$text",
		Sender:    "@alice:example.org",
		Body:      "just chatting",
		Timestamp: time.Now(),
	}})
	view.messages.Refresh()
	if thumb.root.Visible() {
		t.Fatal("text message should not show an inline thumbnail")
	}
}

func TestMediaCacheReportsErrorsAndRetries(t *testing.T) {
	testApp := test.NewApp()
	defer testApp.Quit()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	calls := 0
	cache := newMediaCache(ctx, func(context.Context, string) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("network down")
		}
		return encodedTestPNG(t, 24, 24), nil
	})

	var sawError error
	cache.loadWithError("mxc://example.org/img\x00!room:example.org", func(image.Image) {
		t.Error("first load should not succeed")
	}, func(err error) {
		sawError = err
	})
	if !waitForCondition(func() bool { return sawError != nil }) {
		t.Fatal("error callback was not invoked for a failed fetch")
	}

	var loaded image.Image
	cache.loadWithError("mxc://example.org/img\x00!room:example.org", func(img image.Image) {
		loaded = img
	}, func(err error) {
		t.Errorf("retry should not fail: %v", err)
	})
	if !waitForCondition(func() bool { return loaded != nil }) {
		t.Fatal("failed image should be retried instead of cached forever")
	}
	if got := loaded.Bounds().Size(); got != image.Pt(24, 24) {
		t.Fatalf("retried image size = %v, want 24x24", got)
	}
}

func TestMediaThumbViewLoadsAndShowsErrorState(t *testing.T) {
	testApp := test.NewApp()
	defer testApp.Quit()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := newMediaCache(ctx, func(context.Context, string) ([]byte, error) {
		return nil, errors.New("network down")
	})
	thumb := newMediaThumbView()
	thumb.set("mxc://example.org/img", "!room:example.org", 640, 480, cache, nil)
	if !thumb.root.Visible() {
		t.Fatal("thumbnail should be visible while loading")
	}
	if !waitForCondition(func() bool { return thumb.phLabel.Text == "Unable to load image" }) {
		t.Fatalf("placeholder text = %q, want an error state", thumb.phLabel.Text)
	}
	if thumb.image.Visible() {
		t.Fatal("failed image should stay hidden")
	}
}

func TestMediaThumbViewTapOpensViewer(t *testing.T) {
	testApp := test.NewApp()
	defer testApp.Quit()

	thumb := newMediaThumbView()
	opened := false
	thumb.set("mxc://example.org/img", "!room:example.org", 640, 480, nil, func() {
		opened = true
	})
	if !thumb.image.Tappable() {
		t.Fatal("thumbnail image should be tappable once a message is bound")
	}
	thumb.image.Tapped(nil)
	if !opened {
		t.Fatal("tapping the thumbnail should open the media viewer")
	}
	thumb.hide()
	if thumb.image.Tappable() || thumb.root.Visible() {
		t.Fatal("hidden thumbnail should not stay tappable or visible")
	}
}
