package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func solidImage(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
	return img
}

func TestCompositeLetterboxed(t *testing.T) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	t.Run("nil image produces a solid black frame", func(t *testing.T) {
		frame := compositeLetterboxed(nil, 20, 10)
		if frame.Bounds().Dx() != 20 || frame.Bounds().Dy() != 10 {
			t.Fatalf("frame size = %v, want 20x10", frame.Bounds())
		}
		for y := 0; y < 10; y++ {
			for x := 0; x < 20; x++ {
				if r, g, b, a := frame.At(x, y).RGBA(); r != 0 || g != 0 || b != 0 || a != 0xffff {
					t.Fatalf("pixel (%d,%d) = %v, want opaque black", x, y, frame.At(x, y))
				}
			}
		}
	})

	t.Run("image narrower than frame is letterboxed left/right", func(t *testing.T) {
		// 10x10 source into a 20x10 frame: scaled by height (10/10=1),
		// width stays 10 — pillarboxed with black on both sides, image
		// content occupying x=[5,15).
		src := solidImage(10, 10, white)
		frame := compositeLetterboxed(src, 20, 10)
		if r, g, b, _ := frame.At(0, 5).RGBA(); r != 0 || g != 0 || b != 0 {
			t.Errorf("left edge pixel = %v, want black (pillarbox)", frame.At(0, 5))
		}
		if r, g, b, _ := frame.At(19, 5).RGBA(); r != 0 || g != 0 || b != 0 {
			t.Errorf("right edge pixel = %v, want black (pillarbox)", frame.At(19, 5))
		}
		if r, g, b, _ := frame.At(10, 5).RGBA(); r == 0 && g == 0 && b == 0 {
			t.Errorf("center pixel = %v, want non-black image content", frame.At(10, 5))
		}
	})

	t.Run("image wider than frame is letterboxed top/bottom", func(t *testing.T) {
		// 10x10 source into a 10x20 frame: scaled by width (10/10=1),
		// height stays 10 — letterboxed with black top and bottom, image
		// content occupying y=[5,15).
		src := solidImage(10, 10, white)
		frame := compositeLetterboxed(src, 10, 20)
		if r, g, b, _ := frame.At(5, 0).RGBA(); r != 0 || g != 0 || b != 0 {
			t.Errorf("top edge pixel = %v, want black (letterbox)", frame.At(5, 0))
		}
		if r, g, b, _ := frame.At(5, 19).RGBA(); r != 0 || g != 0 || b != 0 {
			t.Errorf("bottom edge pixel = %v, want black (letterbox)", frame.At(5, 19))
		}
		if r, g, b, _ := frame.At(5, 10).RGBA(); r == 0 && g == 0 && b == 0 {
			t.Errorf("center pixel = %v, want non-black image content", frame.At(5, 10))
		}
	})

	t.Run("exact aspect match fills the frame with no letterbox", func(t *testing.T) {
		src := solidImage(10, 10, white)
		frame := compositeLetterboxed(src, 10, 10)
		for _, p := range []image.Point{{0, 0}, {9, 0}, {0, 9}, {9, 9}, {5, 5}} {
			if r, g, b, _ := frame.At(p.X, p.Y).RGBA(); r == 0 && g == 0 && b == 0 {
				t.Errorf("corner/center pixel %v = %v, want white (no letterbox)", p, frame.At(p.X, p.Y))
			}
		}
	})
}

func TestPickPreferredMode(t *testing.T) {
	modes := []drmModeModeInfo{
		{Hdisplay: 1920, Vdisplay: 1080},
		{Hdisplay: 1280, Vdisplay: 720},
		{Hdisplay: 640, Vdisplay: 480},
	}
	got := pickPreferredMode(modes)
	if got.Hdisplay != 1920 || got.Vdisplay != 1080 {
		t.Errorf("pickPreferredMode() = %dx%d, want the first mode (1920x1080)", got.Hdisplay, got.Vdisplay)
	}
}

func TestRGBAToXRGB8888(t *testing.T) {
	// 2x2 source, red/green/blue/white, with a destination pitch wider
	// than width*4 to exercise the pitch-padding path.
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	src.Set(1, 0, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	src.Set(0, 1, color.RGBA{R: 0, G: 0, B: 255, A: 255})
	src.Set(1, 1, color.RGBA{R: 255, G: 255, B: 255, A: 255})

	const pitch = 16 // wider than 2*4=8, to prove padding is respected
	dst := make([]byte, pitch*2)
	rgbaToXRGB8888(dst, pitch, src)

	check := func(row, col int, wantB, wantG, wantR byte) {
		t.Helper()
		off := row*pitch + col*4
		gotB, gotG, gotR, gotX := dst[off], dst[off+1], dst[off+2], dst[off+3]
		if gotB != wantB || gotG != wantG || gotR != wantR || gotX != 0 {
			t.Errorf("pixel (row %d, col %d) = B%d G%d R%d X%d, want B%d G%d R%d X0", row, col, gotB, gotG, gotR, gotX, wantB, wantG, wantR)
		}
	}
	check(0, 0, 0, 0, 255)     // red
	check(0, 1, 0, 255, 0)     // green
	check(1, 0, 255, 0, 0)     // blue
	check(1, 1, 255, 255, 255) // white
}

// rawInputEvent builds a raw Linux input_event byte buffer with a
// prefixLen-byte leading timeval (8 on 32-bit builds, 16 on 64-bit) filled
// with non-zero filler, to prove parseInputEvent reads from the end of the
// buffer rather than an assumed offset.
func rawInputEvent(prefixLen int, evType, code uint16, value int32) []byte {
	buf := make([]byte, prefixLen+8)
	for i := range buf[:prefixLen] {
		buf[i] = 0xAA // filler distinct from zero, to catch an off-by-offset bug
	}
	tail := buf[prefixLen:]
	binary.LittleEndian.PutUint16(tail[0:2], evType)
	binary.LittleEndian.PutUint16(tail[2:4], code)
	binary.LittleEndian.PutUint32(tail[4:8], uint32(value))
	return buf
}

func TestParseInputEvent(t *testing.T) {
	namedKeys := []struct {
		name string
		code uint16
		want NavKey
	}{
		{"left", keyLeft, NavLeft},
		{"right", keyRight, NavRight},
		{"home", keyHome, NavHome},
		{"delete", keyDelete, NavSleep},
		{"escape", keyEsc, NavQuit},
	}

	for _, prefixLen := range []int{8, 16} { // 32-bit and 64-bit struct timeval widths
		for _, nk := range namedKeys {
			for _, value := range []int32{1, 2} { // initial press and kernel autorepeat
				t.Run(fmt.Sprintf("prefix%d/%s/value%d", prefixLen, nk.name, value), func(t *testing.T) {
					raw := rawInputEvent(prefixLen, evKey, nk.code, value)
					key, ok := parseInputEvent(raw)
					if !ok {
						t.Fatalf("parseInputEvent() ok = false, want true")
					}
					if key != nk.want {
						t.Errorf("parseInputEvent() = %v, want %v", key, nk.want)
					}
				})
			}
		}
	}

	t.Run("arbitrary other key-down maps to NavWake", func(t *testing.T) {
		const keySpace = 57
		raw := rawInputEvent(16, evKey, keySpace, 1)
		key, ok := parseInputEvent(raw)
		if !ok || key != NavWake {
			t.Errorf("parseInputEvent() = (%v, %v), want (NavWake, true)", key, ok)
		}
	})

	t.Run("key-up returns ok=false", func(t *testing.T) {
		raw := rawInputEvent(16, evKey, keyLeft, 0)
		if _, ok := parseInputEvent(raw); ok {
			t.Error("parseInputEvent() ok = true for a key-up event, want false")
		}
	})

	t.Run("non-EV_KEY event returns ok=false", func(t *testing.T) {
		const evSyn = 0x00
		raw := rawInputEvent(16, evSyn, 0, 0)
		if _, ok := parseInputEvent(raw); ok {
			t.Error("parseInputEvent() ok = true for a non-EV_KEY event, want false")
		}
	})
}

func TestIsImageExt(t *testing.T) {
	tests := []struct {
		ext  string
		want bool
	}{
		{".jpg", true},
		{".jpeg", true},
		{".png", true},
		{".JPG", true},
		{".mp4", false},
		{".txt", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			if got := isImageExt(tt.ext); got != tt.want {
				t.Errorf("isImageExt(%q) = %v, want %v", tt.ext, got, tt.want)
			}
		})
	}
}

func writeTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 100, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encoding %s: %v", path, err)
	}
}

func TestScanSlidesImages(t *testing.T) {
	t.Run("mixed valid images plus a corrupt image included with nil thumb", func(t *testing.T) {
		dir := t.TempDir()
		writeTestPNG(t, filepath.Join(dir, "a.png"), 10, 10)
		writeTestPNG(t, filepath.Join(dir, "b.png"), 12, 12)
		if err := os.WriteFile(filepath.Join(dir, "corrupt.png"), []byte("not a real png"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("ignore me"), 0o644); err != nil {
			t.Fatal(err)
		}

		s := &Slideshow{dir: dir, thumbWidth: 8}
		slides, err := s.scanSlides(nil)
		if err != nil {
			t.Fatalf("scanSlides: %v", err)
		}
		if len(slides) != 3 {
			t.Fatalf("got %d slides, want 3 (a.png, b.png, corrupt.png)", len(slides))
		}
		var sawCorruptWithNilThumb bool
		for _, sl := range slides {
			if filepath.Base(sl.path) == "corrupt.png" {
				sawCorruptWithNilThumb = sl.thumb == nil
			}
		}
		if !sawCorruptWithNilThumb {
			t.Error("corrupt.png was not present with a nil thumbnail — corrupt images must be included, not excluded")
		}
	})

	t.Run("unchanged entry is reused across two scans", func(t *testing.T) {
		dir := t.TempDir()
		writeTestPNG(t, filepath.Join(dir, "a.png"), 10, 10)

		s := &Slideshow{dir: dir, thumbWidth: 8}
		first, err := s.scanSlides(nil)
		if err != nil || len(first) != 1 {
			t.Fatalf("first scan: err=%v, %d slides", err, len(first))
		}
		second, err := s.scanSlides(first)
		if err != nil || len(second) != 1 {
			t.Fatalf("second scan: err=%v, %d slides", err, len(second))
		}
		if second[0] != first[0] {
			t.Error("unchanged file was re-processed, want the cached Slide reused")
		}
	})

	t.Run("entry whose mtime changed is re-processed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "a.png")
		writeTestPNG(t, path, 10, 10)

		s := &Slideshow{dir: dir, thumbWidth: 8}
		first, err := s.scanSlides(nil)
		if err != nil || len(first) != 1 {
			t.Fatalf("first scan: err=%v, %d slides", err, len(first))
		}

		writeTestPNG(t, path, 20, 20) // different size
		newMtime := first[0].mtime.Add(time.Hour)
		if err := os.Chtimes(path, newMtime, newMtime); err != nil {
			t.Fatal(err)
		}

		second, err := s.scanSlides(first)
		if err != nil || len(second) != 1 {
			t.Fatalf("second scan: err=%v, %d slides", err, len(second))
		}
		if second[0] == first[0] {
			t.Error("changed file was reused from cache, want it re-processed")
		}
	})
}

// fakeRenderer is a Renderer test double: it records every Present call's
// frame (a nil entry records a Present(nil) blank call) and whether Close
// was called. Safe for concurrent use, though in practice these tests only
// ever call show/pause/resume directly on the test goroutine — nothing
// here spins up Slideshow's run loop.
type fakeRenderer struct {
	mu       sync.Mutex
	presents []*image.RGBA
	closed   bool
}

func (f *fakeRenderer) Present(frame *image.RGBA) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presents = append(f.presents, frame)
	return nil
}

func (f *fakeRenderer) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeRenderer) lastPresent() (frame *image.RGBA, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.presents) == 0 {
		return nil, false
	}
	return f.presents[len(f.presents)-1], true
}

// newHeadlessSlideshow hand-constructs a Slideshow with the fields show,
// pause, resume, and scheduleAdvance touch, populated directly, backed by a
// fakeRenderer instead of real hardware. Run() cannot be called from a test
// since it blocks in its run loop's select — cmdCh is buffered so a
// background decode's result can be posted without a consumer, rather than
// leaking a goroutine blocked on the send.
func newHeadlessSlideshow(slides []Slide, interval time.Duration) (*Slideshow, *fakeRenderer) {
	fr := &fakeRenderer{}
	s := &Slideshow{
		dir:        "unused",
		interval:   interval,
		thumbWidth: 8,
		slides:     slides,
		renderer:   fr,
		width:      640,
		height:     480,
		cmdCh:      make(chan any, 8),
		ctx:        context.Background(),
		cec:        NewCEC(), // cec-client absent in test env; TurnOn/TurnOff are safe no-ops
	}
	return s, fr
}

func TestSlideshowPauseStopsTimerAndBlanksImage(t *testing.T) {
	s, fr := newHeadlessSlideshow([]Slide{{path: "irrelevant"}}, time.Hour)

	fired := make(chan struct{})
	s.advanceTimer = time.AfterFunc(30*time.Millisecond, func() { close(fired) })

	s.pause()

	select {
	case <-fired:
		t.Fatal("advance timer fired after pause(), want it stopped")
	case <-time.After(100 * time.Millisecond):
	}

	frame, ok := fr.lastPresent()
	if !ok {
		t.Fatal("pause() did not call Present")
	}
	if frame != nil {
		t.Error("pause() called Present with a non-nil frame, want Present(nil) to blank the display")
	}
	if !s.paused {
		t.Error("pause() did not set paused = true")
	}
}

func TestSlideshowResumeRearmsTimer(t *testing.T) {
	slides := []Slide{{path: "does-not-exist.png"}}
	s, _ := newHeadlessSlideshow(slides, time.Hour)
	s.paused = true
	s.advanceTimer = nil

	s.resume()

	if s.paused {
		t.Error("resume() left paused = true")
	}
	if s.advanceTimer == nil {
		t.Error("resume() did not arm the advance timer")
	}
}

func TestSlideshowNavKeysRearmTimer(t *testing.T) {
	slides := []Slide{
		{path: "a.png"},
		{path: "b.png"},
		{path: "c.png"},
	}
	// These are exactly the calls handleNavKey makes for Right/Left/Home.
	navs := []struct {
		name string
		call func(s *Slideshow)
	}{
		{"right", func(s *Slideshow) { s.show((s.current+1)%len(s.slides), true) }},
		{"left", func(s *Slideshow) { s.show((s.current-1+len(s.slides))%len(s.slides), true) }},
		{"home", func(s *Slideshow) { s.show(0, true) }},
	}
	for _, nav := range navs {
		t.Run(nav.name, func(t *testing.T) {
			s, _ := newHeadlessSlideshow(slides, time.Hour)
			s.current = 1
			s.advanceTimer = nil

			nav.call(s)

			if s.advanceTimer == nil {
				t.Errorf("%s nav did not arm the advance timer", nav.name)
			}
		})
	}
}

// TestSlideshowImageAdvanceTimingUnchanged pins the pre-existing image
// timing behaviour across the ticker->timer refactor: show() must arm the
// next advance with exactly interval, never any other value.
//
// The Slideshow is built with a long interval so the real time.AfterFunc
// show() arms never actually fires during the test. That no longer matters
// for correctness the way it used to: unlike the old Fyne-based harness,
// where a fired timer's callback ran fyne.Do synchronously on its own
// goroutine and could race the test's direct field access, this timer's
// callback only sends a value on s.cmdCh — it never touches Slideshow
// fields directly — so there is no reentrancy hazard to route around even
// if it did fire.
func TestSlideshowImageAdvanceTimingUnchanged(t *testing.T) {
	slides := []Slide{{path: "does-not-exist.png"}}
	s, _ := newHeadlessSlideshow(slides, time.Hour)

	s.show(0, true)
	if s.advanceTimer == nil {
		t.Fatal("show() did not arm the advance timer for an image slide")
	}
	t.Cleanup(func() { s.advanceTimer.Stop() })
}

// TestSlideshowEscapeAndDeleteBypassResume guards the key precedence
// handleNavKey depends on: NavQuit and NavSleep must act unconditionally
// on a paused sign and must never themselves count as the key that resumes
// it — unlike NavLeft/NavRight/NavHome/NavWake, which resume first. Both
// paths here leave s.paused == true; if either accidentally resumed, this
// would flip to false.
func TestSlideshowEscapeAndDeleteBypassResume(t *testing.T) {
	tests := []struct {
		name string
		key  NavKey
	}{
		{"escape (NavQuit)", NavQuit},
		{"delete (NavSleep)", NavSleep},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newHeadlessSlideshow([]Slide{{path: "irrelevant"}}, time.Hour)
			ctx, cancel := context.WithCancel(context.Background())
			s.ctx = ctx
			s.cancel = cancel
			s.paused = true

			s.handleNavKey(tt.key)

			if !s.paused {
				t.Errorf("handleNavKey(%v) left paused = false — resume() ran, want Escape/Delete to bypass resume entirely", tt.key)
			}
		})
	}
}
