package ui

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"

	"fyne.io/fyne/v2"
)

//go:embed assets/matrix.png
var appIcon []byte

func iconResource() fyne.Resource {
	return fyne.NewStaticResource("matrix.png", appIcon)
}

func unreadIconResource() fyne.Resource {
	source, err := png.Decode(bytes.NewReader(appIcon))
	if err != nil {
		log.Printf("decode embedded application icon for unread tray badge: %v", err)
		return iconResource()
	}
	bounds := source.Bounds()
	badged := image.NewRGBA(bounds)
	draw.Draw(badged, bounds, source, bounds.Min, draw.Src)
	centerX, centerY := bounds.Max.X-30, bounds.Min.Y+30
	for y := centerY - 18; y <= centerY+18; y++ {
		for x := centerX - 18; x <= centerX+18; x++ {
			dx, dy := x-centerX, y-centerY
			if dx*dx+dy*dy <= 18*18 {
				badged.Set(x, y, color.NRGBA{R: 239, G: 68, B: 68, A: 255})
			}
		}
	}
	var output bytes.Buffer
	if err = png.Encode(&output, badged); err != nil {
		log.Printf("encode application icon with unread tray badge: %v", err)
		return iconResource()
	}
	return fyne.NewStaticResource("matrix-unread.png", output.Bytes())
}
