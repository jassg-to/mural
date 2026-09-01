package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
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

// writeBenchPNG writes a w×h solid-color PNG to path, for benchmarks that
// need real files on disk (image.Decode requires a real codec-readable
// source) but can't use writeTestPNG from slideshow_test.go, which takes a
// *testing.T rather than a *testing.B.
func writeBenchPNG(b *testing.B, path string, w, h int) {
	b.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	f, err := os.Create(path)
	if err != nil {
		b.Fatalf("creating %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		b.Fatalf("encoding %s: %v", path, err)
	}
}

// BenchmarkStartupScan is the regression guard for this change's whole
// premise: scanSlidePaths (the fast path Run() now uses at startup) must
// stay far cheaper than scanSlides (which eagerly decodes every
// thumbnail) as the directory's slide count and resolution grow — that
// gap is what lets the first slide reach the screen quickly regardless of
// how many other images are alongside it. 12 images at 1920x1080
// approximates the real content directory this fix was written for
// (docs/kit.jpg-class content, a dozen full-HD PNGs).
func BenchmarkStartupScan(b *testing.B) {
	dir := b.TempDir()
	const n = 12
	for i := 0; i < n; i++ {
		writeBenchPNG(b, filepath.Join(dir, fmt.Sprintf("%02d.png", i)), 1920, 1080)
	}

	b.Run("scanSlidePaths(fast_path)", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := scanSlidePaths(dir); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("scanSlides(decodes_every_thumbnail)", func(b *testing.B) {
		s := &Slideshow{dir: dir, thumbWidth: 80}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := s.scanSlides(nil, 80); err != nil {
				b.Fatal(err)
			}
		}
	})
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
