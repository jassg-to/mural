package main

import (
	"image"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
)

// compositeLetterboxed returns a w×h RGBA frame with img centered and
// letterboxed against black. img == nil (corrupt source, or a blank
// request) produces a solid black frame — this is Mural's own definition
// of "corrupt image's frame".
func compositeLetterboxed(img image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.Black, image.Point{}, draw.Src)

	if img == nil {
		return dst
	}

	bounds := img.Bounds()
	imgW := float64(bounds.Dx())
	imgH := float64(bounds.Dy())
	if imgW <= 0 || imgH <= 0 {
		return dst
	}

	scale := math.Min(float64(w)/imgW, float64(h)/imgH)
	targetW := int(math.Round(imgW * scale))
	targetH := int(math.Round(imgH * scale))
	if targetW <= 0 || targetH <= 0 {
		return dst
	}

	offX := (w - targetW) / 2
	offY := (h - targetH) / 2
	dstRect := image.Rect(offX, offY, offX+targetW, offY+targetH)
	xdraw.CatmullRom.Scale(dst, dstRect, img, bounds, xdraw.Src, nil)

	return dst
}
