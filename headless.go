package main

import (
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
)

// headlessOutputPath is where HeadlessRenderer dumps each composited frame
// for a developer to inspect off the sign. Fixed and undocumented as
// configurable — nothing in this feature needs it to be.
const headlessOutputPath = "mural-headless.png"

// headlessWidth and headlessHeight are the fixed frame size HeadlessRenderer
// reports. Arbitrary and dev-only: HeadlessRenderer has no real connector to
// discover a mode from.
const (
	headlessWidth  = 1920
	headlessHeight = 1080
)

// HeadlessRenderer is the developer's-machine Renderer: it PNG-dumps each
// composited frame to a fixed path on disk instead of driving real
// hardware, so Mural can run and be exercised off the sign.
type HeadlessRenderer struct {
	path string
}

// NewHeadlessRenderer returns a HeadlessRenderer and the fixed frame size
// it reports.
func NewHeadlessRenderer() (*HeadlessRenderer, int, int) {
	log.Printf("headless renderer: writing frames to %s", headlessOutputPath)
	return &HeadlessRenderer{path: headlessOutputPath}, headlessWidth, headlessHeight
}

// Present PNG-encodes the composited frame (or black, if frame is nil) to
// h's fixed output path.
func (h *HeadlessRenderer) Present(frame *image.RGBA) error {
	src := image.Image(frame)
	if frame == nil {
		src = compositeLetterboxed(nil, headlessWidth, headlessHeight)
	}

	f, err := os.Create(h.path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", h.path, err)
	}
	defer f.Close()

	if err := png.Encode(f, src); err != nil {
		return fmt.Errorf("encoding %s: %w", h.path, err)
	}
	return nil
}

// Close is a no-op: HeadlessRenderer owns no hardware resource.
func (h *HeadlessRenderer) Close() error {
	return nil
}
