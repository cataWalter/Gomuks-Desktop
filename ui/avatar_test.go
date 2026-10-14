package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestDecodeAvatar(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 24, 24))); err != nil {
		t.Fatalf("encode test avatar: %v", err)
	}

	decoded, err := decodeAvatar(encoded.Bytes())
	if err != nil {
		t.Fatalf("decode valid avatar: %v", err)
	}
	if got := decoded.Bounds().Size(); got != image.Pt(24, 24) {
		t.Fatalf("decoded avatar size = %v, want 24x24", got)
	}
}

func TestDecodeAvatarResizesForUI(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 256, 128))); err != nil {
		t.Fatalf("encode test avatar: %v", err)
	}

	decoded, err := decodeAvatar(encoded.Bytes())
	if err != nil {
		t.Fatalf("decode avatar: %v", err)
	}
	if got := decoded.Bounds().Size(); got != image.Pt(96, 48) {
		t.Fatalf("resized avatar size = %v, want 96x48", got)
	}
}

func TestDecodeAvatarRejectsOversizedDimensions(t *testing.T) {
	var encoded bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2049, 1))
	img.Set(0, 0, color.White)
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatalf("encode oversized test avatar: %v", err)
	}

	if _, err := decodeAvatar(encoded.Bytes()); err == nil {
		t.Fatal("expected oversized avatar to be rejected")
	}
}
