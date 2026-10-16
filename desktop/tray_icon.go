package desktop

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

//go:embed tray.png
var baseTrayIcon []byte

func trayIcons() ([]byte, []byte) {
	source, err := png.Decode(bytes.NewReader(baseTrayIcon))
	if err != nil {
		return baseTrayIcon, baseTrayIcon
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
		return baseTrayIcon, baseTrayIcon
	}
	return baseTrayIcon, output.Bytes()
}
