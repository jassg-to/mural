package main

import "image"

// Renderer is the display sink Slideshow presents composited frames to.
// Present(nil) blanks the display to black.
type Renderer interface {
	Present(frame *image.RGBA) error
	Close() error
}
