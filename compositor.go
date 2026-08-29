package main

import (
	"image"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
)

// compositeLetterboxed returns a w×h RGBA frame with img centered and
// letterboxed against black, resampled with scaler. img == nil (corrupt
// source, or a blank request) produces a solid black frame — this is
// Mural's own definition of "corrupt image's frame".
//
// The caller picks scaler because the right tradeoff differs by call site:
// profiling on real hardware (a Pi 3B+) found compositeLetterboxed's
// CatmullRom pass dominating per-keypress latency when it's used to
// upscale the small instant-nav thumbnail (~24x, 80px wide to a ~1920px
// display) — a controlled benchmark (bench_test.go) measured
// xdraw.NearestNeighbor roughly 4.1x faster than CatmullRom for that case
// (113ms vs 466ms per call), and quality doesn't matter for a placeholder
// on screen for a fraction of a second. The full decoded image that
// replaces it and stays on screen for the whole interval keeps
// xdraw.CatmullRom: decodeAndFit has already scaled it to fit, so this
// pass is a near-identity resize regardless of scaler, and it's not on
// the latency-critical path.
func compositeLetterboxed(img image.Image, w, h int, scaler xdraw.Scaler) *image.RGBA {
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
	scaler.Scale(dst, dstRect, img, bounds, xdraw.Src, nil)

	return dst
}
