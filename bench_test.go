package main

import (
	"fmt"
	"image/color"
	"testing"

	xdraw "golang.org/x/image/draw"
)

// BenchmarkCompositeLetterboxedNN measures compositeLetterboxed's
// NearestNeighbor cost (the instant-nav thumbnail path) across a range of
// thumb_width values, at a fixed 1920x1200 output size — isolating
// thumb_width's effect from human keypress-timing noise.
func BenchmarkCompositeLetterboxedNN(b *testing.B) {
	for _, w := range []int{40, 80, 160, 240, 320, 480} {
		thumb := solidImage(w, w*9/16, color.RGBA{R: 255, A: 255})
		b.Run(fmt.Sprintf("thumbWidth=%d", w), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = compositeLetterboxed(thumb, 1920, 1200, xdraw.NearestNeighbor)
			}
		})
	}
}

// BenchmarkCompositeLetterboxedScaler compares the three candidate scalers
// under identical, controlled conditions (fixed 80px thumbnail, fixed
// 1920x1200 output) — a precise version of the earlier profile-sample-count
// estimate, which was a rough division, not a real measurement.
func BenchmarkCompositeLetterboxedScaler(b *testing.B) {
	thumb := solidImage(80, 45, color.RGBA{R: 255, A: 255})
	scalers := map[string]xdraw.Scaler{
		"NearestNeighbor": xdraw.NearestNeighbor,
		"ApproxBiLinear":  xdraw.ApproxBiLinear,
		"CatmullRom":      xdraw.CatmullRom,
	}
	for name, scaler := range scalers {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = compositeLetterboxed(thumb, 1920, 1200, scaler)
			}
		})
	}
}
