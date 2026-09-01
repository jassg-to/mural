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

	xdraw "golang.org/x/image/draw"
)

func solidImage(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
	return img
}

func TestCompositeLetterboxed(t *testing.T) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}

	t.Run("nil image produces a solid black frame", func(t *testing.T) {
		frame := compositeLetterboxed(nil, 20, 10, xdraw.CatmullRom)
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
		frame := compositeLetterboxed(src, 20, 10, xdraw.CatmullRom)
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
		frame := compositeLetterboxed(src, 10, 20, xdraw.CatmullRom)
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
		frame := compositeLetterboxed(src, 10, 10, xdraw.CatmullRom)
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

func TestHasAnyOnWindow(t *testing.T) {
	tests := []struct {
		name string
		cfg  ScheduleConfig
		want bool
	}{
		{
			name: "zero-value config",
			cfg:  ScheduleConfig{},
			want: false,
		},
		{
			name: "single all window",
			cfg:  ScheduleConfig{Monday: DayConfig{All: []Window{{On: 8 * 60, Off: 20 * 60}}}},
			want: true,
		},
		{
			name: "zero-length window only",
			cfg:  ScheduleConfig{Tuesday: DayConfig{All: []Window{{On: 18 * 60, Off: 18 * 60}}}},
			want: false,
		},
		{
			name: "overnight window only",
			cfg:  ScheduleConfig{Wednesday: DayConfig{All: []Window{{On: 22 * 60, Off: 2 * 60}}}},
			want: false,
		},
		{
			name: "occurrence-only window with no all",
			cfg:  ScheduleConfig{Thursday: DayConfig{Second: []Window{{On: 9 * 60, Off: 17 * 60}}}},
			want: true,
		},
		{
			name: "mix of one zero-length and one real window",
			cfg: ScheduleConfig{Friday: DayConfig{All: []Window{
				{On: 12 * 60, Off: 12 * 60},
				{On: 8 * 60, Off: 20 * 60},
			}}},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasAnyOnWindow(tt.cfg); got != tt.want {
				t.Errorf("hasAnyOnWindow(%+v) = %v, want %v", tt.cfg, got, tt.want)
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
		slides, err := s.scanSlides(nil, s.thumbWidth)
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
		first, err := s.scanSlides(nil, s.thumbWidth)
		if err != nil || len(first) != 1 {
			t.Fatalf("first scan: err=%v, %d slides", err, len(first))
		}
		second, err := s.scanSlides(first, s.thumbWidth)
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
		first, err := s.scanSlides(nil, s.thumbWidth)
		if err != nil || len(first) != 1 {
			t.Fatalf("first scan: err=%v, %d slides", err, len(first))
		}

		writeTestPNG(t, path, 20, 20) // different size
		newMtime := first[0].mtime.Add(time.Hour)
		if err := os.Chtimes(path, newMtime, newMtime); err != nil {
			t.Fatal(err)
		}

		second, err := s.scanSlides(first, s.thumbWidth)
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

// TestWaitingStateNavKeysDoNotPanic covers the two %n divide-by-zero sites
// in handleNavKey (Architect.md "The waiting state is where the sharp
// edges are"): NavLeft/NavRight/NavHome on an empty rotation must return
// without acting rather than computing (s.current±1) % 0.
func TestWaitingStateNavKeysDoNotPanic(t *testing.T) {
	for _, key := range []NavKey{NavLeft, NavRight, NavHome} {
		t.Run(fmt.Sprintf("key=%d", key), func(t *testing.T) {
			s, fr := newHeadlessSlideshow(nil, time.Hour)
			ctx, cancel := context.WithCancel(context.Background())
			s.ctx = ctx
			s.cancel = cancel
			before := len(fr.presents)

			s.handleNavKey(key) // must not panic

			if len(fr.presents) != before {
				t.Errorf("handleNavKey(%v) on empty rotation presented something new, want no-op", key)
			}
		})
	}
}

// TestWaitingStateAdvanceDoesNotPanic covers advanceToNext's %n site: an
// advanceMsg arriving while waiting must be a no-op, not a divide-by-zero.
func TestWaitingStateAdvanceDoesNotPanic(t *testing.T) {
	s, _ := newHeadlessSlideshow(nil, time.Hour)
	s.advanceToNext() // must not panic
}

// TestWaitingStateResumeShowsBlank confirms resume() still un-pauses and
// presents a blank frame on an empty rotation — Architect.md is explicit
// that resume() must not be skipped while waiting, since a config-only
// ingest arriving at an empty sign wakes the display as its only
// confirmation that the stick was read at all.
func TestWaitingStateResumeShowsBlank(t *testing.T) {
	s, fr := newHeadlessSlideshow(nil, time.Hour)
	s.paused = true

	s.resume()

	if s.paused {
		t.Error("resume() left paused = true")
	}
	frame, ok := fr.lastPresent()
	if !ok {
		t.Fatal("resume() on an empty rotation did not call Present")
	}
	if frame != nil {
		t.Error("resume() on an empty rotation presented a non-blank frame, want Present(nil)")
	}
}

// TestWaitingStateVTAcquireDoesNotPanic covers the fourth show() caller
// Architect.md names as easy to miss: handleVTEvent(vtEventAcquired) on an
// empty rotation is the same out-of-range panic as the other three sites,
// covered by show()'s own guard but asserted here rather than assumed — a
// VT switch on a waiting sign is exactly the unattended-kiosk scenario
// nobody exercises by hand.
func TestWaitingStateVTAcquireDoesNotPanic(t *testing.T) {
	s, _ := newHeadlessSlideshow(nil, time.Hour)
	s.handleVTEvent(vtEventAcquired) // must not panic
}

// newWaitingSlideshow builds a Slideshow over a real, empty t.TempDir()
// content directory with a cancellable context, for exercising Run()
// itself against an empty directory. newHeadlessSlideshow cannot be
// reused for this: its own doc comment states Run() cannot be called from
// a test (it blocks in its run loop's select) and it hands back an
// uncancellable context.Background() — the opposite of what this helper
// needs from ctx.
func newWaitingSlideshow(t *testing.T) (*Slideshow, context.CancelFunc, *fakeRenderer) {
	t.Helper()
	dir := t.TempDir()
	fr := &fakeRenderer{}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Slideshow{
		dir:        dir,
		interval:   time.Hour,
		thumbWidth: 8,
		renderer:   fr,
		width:      640,
		height:     480,
		cmdCh:      make(chan any, 8),
		ctx:        ctx,
		cec:        NewCEC(),
	}
	return s, cancel, fr
}

// TestRunStartsAndWaitsOnEmptyContentDir covers the Delta's headline
// change to Run: an empty content directory must no longer be a fatal
// startup error. The player starts, reaches the waiting state, and keeps
// running the select loop normally until cancelled.
func TestRunStartsAndWaitsOnEmptyContentDir(t *testing.T) {
	s, cancel, fr := newWaitingSlideshow(t)

	runErr := make(chan error, 1)
	go func() {
		runErr <- s.Run(cancel, nil, nil, nil)
	}()

	// Give Run a moment to reach its select loop, then confirm it did not
	// exit with an error (which is what the old fatal-on-empty-dir
	// behaviour would have produced almost immediately). s.slides/s.current
	// are owned by the run-loop goroutine once Run starts, so this checks
	// the waiting state through the thread-safe fakeRenderer instead of
	// touching Slideshow fields directly from the test goroutine.
	select {
	case err := <-runErr:
		t.Fatalf("Run returned early with err=%v, want it to keep running and waiting for content", err)
	case <-time.After(100 * time.Millisecond):
	}
	frame, ok := fr.lastPresent()
	if !ok {
		t.Fatal("Run did not present anything for an empty content directory, want a blank Present(nil)")
	}
	if frame != nil {
		t.Error("Run presented a non-blank frame for an empty content directory, want the waiting state's Present(nil)")
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("Run returned err=%v after cancellation, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of cancellation")
	}
}

// TestReloadResultWakeControlsResume covers the rescan/wake split (Step
// 19): the result handler must resume a paused sign only when wake is
// set, exercising Reload()'s wake:true path and an ingest-triggered
// rescan's wake:false-on-failure path alike.
func TestReloadResultWakeControlsResume(t *testing.T) {
	tests := []struct {
		name        string
		wake        bool
		wantResumed bool
	}{
		{"wake=false leaves a paused sign paused", false, false},
		{"wake=true resumes a paused sign", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newHeadlessSlideshow([]Slide{{path: "a"}}, time.Hour)
			s.paused = true

			s.handleCommand(reloadResultMsg{slides: []Slide{{path: "b"}, {path: "c"}}, wake: tt.wake})

			if resumed := !s.paused; resumed != tt.wantResumed {
				t.Errorf("paused after wake=%v result = %v, want resumed=%v", tt.wake, s.paused, tt.wantResumed)
			}
		})
	}
}

// TestReloadResultEmptyAdoptsWaitingState covers Step 20: an empty rescan
// result must adopt the empty set (entering the waiting state) rather
// than keeping the stale previous slides, and — because a config-only
// ingest arriving at an empty sign wakes the display as the operator's
// only confirmation the stick was read — must still resume when wake is
// set.
func TestReloadResultEmptyAdoptsWaitingState(t *testing.T) {
	t.Run("wake=true adopts empty set and resumes", func(t *testing.T) {
		s, fr := newHeadlessSlideshow([]Slide{{path: "a"}}, time.Hour)
		s.paused = true

		s.handleCommand(reloadResultMsg{slides: nil, wake: true})

		if !s.waiting() {
			t.Error("empty rescan result did not adopt the empty set")
		}
		if s.paused {
			t.Error("wake=true on an empty rescan result did not resume")
		}
		frame, ok := fr.lastPresent()
		if !ok || frame != nil {
			t.Errorf("resume on empty rotation: lastPresent = (%v, %v), want (nil, true)", frame, ok)
		}
	})

	t.Run("wake=false adopts empty set without resuming", func(t *testing.T) {
		s, _ := newHeadlessSlideshow([]Slide{{path: "a"}}, time.Hour)
		s.paused = true

		s.handleCommand(reloadResultMsg{slides: nil, wake: false})

		if !s.waiting() {
			t.Error("empty rescan result did not adopt the empty set")
		}
		if !s.paused {
			t.Error("wake=false on an empty rescan result resumed, want it to stay paused")
		}
	})
}

// TestApplySlideshowConfigUsesNewIntervalAndWidth covers Step 21:
// applySlideshowConfig's assignments must actually be what the next
// scheduleAdvance and the next rescan use, not just fields that happen to
// be set.
func TestApplySlideshowConfigUsesNewIntervalAndWidth(t *testing.T) {
	s, _ := newHeadlessSlideshow([]Slide{{path: "irrelevant"}}, time.Hour)

	s.applySlideshowConfig(SlideshowConfig{Interval: Duration(20 * time.Millisecond), ThumbWidth: 42})
	if s.thumbWidth != 42 {
		t.Fatalf("thumbWidth = %d, want 42", s.thumbWidth)
	}

	s.scheduleAdvance(s.interval)
	select {
	case msg := <-s.cmdCh:
		if _, ok := msg.(advanceMsg); !ok {
			t.Fatalf("got %T, want advanceMsg", msg)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("advance timer did not fire promptly for a 20ms interval — applySlideshowConfig's interval was not used")
	}

	dir := t.TempDir()
	writeTestPNG(t, filepath.Join(dir, "a.png"), 10, 10)
	s.dir = dir
	slides, err := s.scanSlides(nil, s.thumbWidth)
	if err != nil {
		t.Fatalf("scanSlides: %v", err)
	}
	if got := slides[0].thumb.Bounds().Dx(); got != 42 {
		t.Errorf("thumbnail width = %d, want 42 (applySlideshowConfig's width)", got)
	}
}

// TestScheduleApplyConfig covers Step 23's two guarantees: the new config
// takes effect immediately (not just eventually), and repeated calls never
// block even with nothing draining replan — the direct regression test
// for signalReplan's non-blocking select/default send.
func TestScheduleApplyConfig(t *testing.T) {
	t.Run("IsOn reflects the new config immediately", func(t *testing.T) {
		sched := NewSchedule("unused", ScheduleConfig{}, func() {}, func() {})
		now := time.Now()
		noon := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location())

		if sched.IsOn(noon) {
			t.Fatal("zero-value schedule should report off at noon")
		}

		allDay := DayConfig{All: []Window{{On: 0, Off: 23*60 + 59}}}
		cfg := ScheduleConfig{
			Monday: allDay, Tuesday: allDay, Wednesday: allDay, Thursday: allDay,
			Friday: allDay, Saturday: allDay, Sunday: allDay,
		}
		sched.ApplyConfig(cfg)

		if !sched.IsOn(noon) {
			t.Error("IsOn(noon) after ApplyConfig = false, want true — the new config should apply immediately")
		}
	})

	t.Run("repeated ApplyConfig does not block", func(t *testing.T) {
		sched := NewSchedule("unused", ScheduleConfig{}, func() {}, func() {})

		done := make(chan struct{})
		go func() {
			for i := 0; i < 5; i++ {
				sched.ApplyConfig(ScheduleConfig{}) // nothing ever drains sched.replan
			}
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("ApplyConfig blocked with nothing draining replan — signalReplan must be a non-blocking send")
		}
	})
}

// TestFutureEvents asserts the no-double-fire property against the pure
// filter extracted in Step 23, using a hand-built event list and a fixed
// now — Schedule has no injectable clock, so asserting this by calling
// ApplyConfig and observing a callback would be a wall-clock race that
// flakes in CI.
func TestFutureEvents(t *testing.T) {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	events := []event{
		{at: base.Add(-2 * time.Hour), turnOn: true},
		{at: base.Add(-1 * time.Hour), turnOn: false},
		{at: base.Add(1 * time.Hour), turnOn: true},
		{at: base.Add(2 * time.Hour), turnOn: false},
	}

	got := futureEvents(events, base)

	if len(got) != 2 {
		t.Fatalf("got %d future events, want 2 (past events dropped): %+v", len(got), got)
	}
	if !got[0].at.Equal(base.Add(1*time.Hour)) || !got[0].turnOn {
		t.Errorf("first future event = %+v, want on@+1h", got[0])
	}
	if !got[1].at.Equal(base.Add(2*time.Hour)) || got[1].turnOn {
		t.Errorf("second future event = %+v, want off@+2h", got[1])
	}
}

// newIngestTestSlideshow builds a Slideshow over a real t.TempDir() content
// directory (so startRescan's background goroutine has somewhere real to
// scan) with a buffered cmdCh, for driving handleMediaMount and
// handleIngestResult directly — never through Run, which cannot be called
// from a test.
func newIngestTestSlideshow(t *testing.T, initialSlides []Slide) (*Slideshow, *fakeRenderer) {
	t.Helper()
	dir := t.TempDir()
	writeTestPNG(t, filepath.Join(dir, "existing.png"), 4, 4)
	fr := &fakeRenderer{}
	s := &Slideshow{
		dir:        dir,
		interval:   time.Hour,
		thumbWidth: 8,
		slides:     initialSlides,
		renderer:   fr,
		width:      640,
		height:     480,
		cmdCh:      make(chan any, 8),
		ctx:        context.Background(),
		cec:        NewCEC(),
		ingestFn:   ingestVolume,
	}
	return s, fr
}

// drainAndHandle receives one message from s.cmdCh (failing the test if
// none arrives within a short timeout) and feeds it through handleCommand,
// simulating one iteration of the run loop's select.
func drainAndHandle(t *testing.T, s *Slideshow) any {
	t.Helper()
	select {
	case msg := <-s.cmdCh:
		s.handleCommand(msg)
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("nothing arrived on cmdCh within 2s")
		return nil
	}
}

func assertNothingOnCmdCh(t *testing.T, s *Slideshow) {
	t.Helper()
	select {
	case msg := <-s.cmdCh:
		t.Errorf("got unexpected message %T on cmdCh, want none", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHandleIngestResultAcceptedRescansAndResumes(t *testing.T) {
	s, fr := newIngestTestSlideshow(t, []Slide{{path: "old"}})
	s.paused = true

	cfg := &Config{}
	s.handleIngestResult(ingestResultMsg{
		mountPoint: "/media/mural/stick",
		result:     ingestResult{disposition: volumeAccepted, cfg: cfg, imagesApplied: true},
	})

	// The rescan runs on a background goroutine; drain its reloadResultMsg
	// and feed it through handleCommand, as the run loop would.
	msg := drainAndHandle(t, s)
	if _, ok := msg.(reloadResultMsg); !ok {
		t.Fatalf("got %T after accepted result, want reloadResultMsg (from startRescan)", msg)
	}

	if s.paused {
		t.Error("accepted result did not resume a paused sign")
	}
	if len(s.slides) != 1 || filepath.Base(s.slides[0].path) != "existing.png" {
		t.Errorf("slides after accepted-result rescan = %+v, want the rescanned content directory", s.slides)
	}
	if _, ok := fr.lastPresent(); !ok {
		t.Error("accepted result's resume did not present anything")
	}
}

func TestHandleIngestResultIgnoredAndRejectedChangeNothing(t *testing.T) {
	for _, tt := range []struct {
		name string
		res  ingestResult
	}{
		{"ignored", ingestResult{disposition: volumeIgnored}},
		{"rejected", ingestResult{disposition: volumeRejected, err: fmt.Errorf("bad config")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			initial := []Slide{{path: "old1"}, {path: "old2"}}
			s, fr := newIngestTestSlideshow(t, initial)
			s.paused = true
			presentsBefore := len(fr.presents)

			s.handleIngestResult(ingestResultMsg{mountPoint: "/media/mural/stick", result: tt.res})

			assertNothingOnCmdCh(t, s) // no rescan/resume triggered
			if !s.paused {
				t.Error("paused sign was resumed, want untouched")
			}
			if len(s.slides) != len(initial) || s.slides[0] != initial[0] || s.slides[1] != initial[1] {
				t.Errorf("slides changed: got %+v, want %+v", s.slides, initial)
			}
			if len(fr.presents) != presentsBefore {
				t.Error("renderer received a new Present call, want none")
			}
		})
	}
}

func TestHandleIngestResultAcceptedButFailed(t *testing.T) {
	t.Run("mutated=true rescans without resuming", func(t *testing.T) {
		s, _ := newIngestTestSlideshow(t, []Slide{{path: "old"}})
		s.paused = true

		s.handleIngestResult(ingestResultMsg{
			mountPoint: "/media/mural/stick",
			result:     ingestResult{disposition: volumeAccepted, err: fmt.Errorf("commit failed"), mutated: true},
		})

		msg := drainAndHandle(t, s)
		rr, ok := msg.(reloadResultMsg)
		if !ok {
			t.Fatalf("got %T, want reloadResultMsg (repair rescan)", msg)
		}
		if rr.wake {
			t.Error("repair rescan after a failed ingest must not wake the display (wake=false)")
		}
		if !s.paused {
			t.Error("repair rescan resumed a paused sign, want it to stay paused (failed ingest must not wake)")
		}
	})

	t.Run("mutated=false does nothing at all", func(t *testing.T) {
		initial := []Slide{{path: "old"}}
		s, fr := newIngestTestSlideshow(t, initial)
		s.paused = true
		presentsBefore := len(fr.presents)

		s.handleIngestResult(ingestResultMsg{
			mountPoint: "/media/mural/stick",
			result:     ingestResult{disposition: volumeAccepted, err: fmt.Errorf("staging failed"), mutated: false},
		})

		assertNothingOnCmdCh(t, s)
		if !s.paused {
			t.Error("paused sign was resumed, want untouched")
		}
		if len(s.slides) != 1 || s.slides[0] != initial[0] {
			t.Errorf("slides changed: got %+v, want %+v", s.slides, initial)
		}
		if len(fr.presents) != presentsBefore {
			t.Error("renderer received a new Present call, want none")
		}
	})
}

// TestHandleMediaMountQueue drives handleMediaMount and the
// ingestResultMsg handler directly (Run cannot be called from a test) to
// prove two properties Architect.md requires of the queue: two mount
// points are processed strictly one at a time, and the same mount point
// arriving twice while an ingest is in flight is enqueued at most once.
func TestHandleMediaMountQueue(t *testing.T) {
	t.Run("two mount points are processed one at a time", func(t *testing.T) {
		s, _ := newIngestTestSlideshow(t, nil)
		started := make(chan string, 4)
		release := make(chan struct{})
		s.ingestFn = func(ctx context.Context, mp, dir string, avail func(string) (uint64, error), onCommit func()) ingestResult {
			started <- mp
			<-release
			return ingestResult{disposition: volumeIgnored}
		}

		s.handleMediaMount("/media/mural/a")
		select {
		case got := <-started:
			if got != "/media/mural/a" {
				t.Fatalf("first ingest started for %q, want /media/mural/a", got)
			}
		case <-time.After(time.Second):
			t.Fatal("first mount point never started an ingest")
		}

		s.handleMediaMount("/media/mural/b")
		select {
		case got := <-started:
			t.Fatalf("second mount point %q started while the first was still in flight", got)
		case <-time.After(100 * time.Millisecond):
		}
		if len(s.pendingMounts) != 1 || s.pendingMounts[0] != "/media/mural/b" {
			t.Fatalf("pendingMounts = %v, want [/media/mural/b]", s.pendingMounts)
		}

		release <- struct{}{} // let the first ingest finish
		drainAndHandle(t, s)  // its ingestResultMsg: clears ingesting, starts the queued second

		select {
		case got := <-started:
			if got != "/media/mural/b" {
				t.Fatalf("second ingest started for %q, want /media/mural/b", got)
			}
		case <-time.After(time.Second):
			t.Fatal("queued second mount point never started after the first finished")
		}
		release <- struct{}{}
		drainAndHandle(t, s)
	})

	t.Run("the same mount point arriving twice while in flight is enqueued once", func(t *testing.T) {
		s, _ := newIngestTestSlideshow(t, nil)
		started := make(chan string, 4)
		release := make(chan struct{})
		s.ingestFn = func(ctx context.Context, mp, dir string, avail func(string) (uint64, error), onCommit func()) ingestResult {
			started <- mp
			<-release
			return ingestResult{disposition: volumeIgnored}
		}

		s.handleMediaMount("/media/mural/a")
		<-started // "a" is now in flight

		s.handleMediaMount("/media/mural/b")
		s.handleMediaMount("/media/mural/b") // duplicate while queued, not in flight

		if len(s.pendingMounts) != 1 || s.pendingMounts[0] != "/media/mural/b" {
			t.Fatalf("pendingMounts = %v, want exactly one /media/mural/b", s.pendingMounts)
		}

		release <- struct{}{}
		drainAndHandle(t, s)

		select {
		case got := <-started:
			if got != "/media/mural/b" {
				t.Fatalf("started ingest for %q, want /media/mural/b", got)
			}
		case <-time.After(time.Second):
			t.Fatal("queued mount point never started")
		}
		release <- struct{}{}
		drainAndHandle(t, s)
	})
}

// TestHandleIngestResultAcceptedNeverSendsOnUnbufferedCmdCh is the
// deadlock regression Step 21 exists to prevent: the ingest-result handler
// runs on the run-loop goroutine itself, and cmdCh is unbuffered with the
// run loop as its only reader, so if the accepted branch ever called the
// posting ApplyConfig instead of the direct applySlideshowConfig, this
// call would hang forever (the sign frozen) rather than returning. A
// buffered test channel cannot reproduce this — capacity absorbs the send
// and the test would pass either way — so this constructs an unbuffered
// cmdCh deliberately.
//
// ctx is cancelled only in t.Cleanup, after the pass/fail race below is
// decided: postCommand's select also has a <-s.ctx.Done() arm, so an
// already-cancelled context would let even the buggy form return, defeating
// the regression test entirely.
func TestHandleIngestResultAcceptedNeverSendsOnUnbufferedCmdCh(t *testing.T) {
	dir := t.TempDir()
	writeTestPNG(t, filepath.Join(dir, "a.png"), 4, 4)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s := &Slideshow{
		dir:        dir,
		interval:   time.Hour,
		thumbWidth: 8,
		renderer:   &fakeRenderer{},
		width:      640,
		height:     480,
		cmdCh:      make(chan any), // unbuffered — reproduces the Step 21 hazard
		ctx:        ctx,
		cec:        NewCEC(),
	}

	done := make(chan struct{})
	go func() {
		s.handleCommand(ingestResultMsg{
			mountPoint: dir,
			result:     ingestResult{disposition: volumeAccepted, cfg: &Config{}, imagesApplied: true},
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handleCommand(ingestResultMsg{accepted}) did not return within 1s — it must never synchronously send on cmdCh")
	}
}

func TestSlideshowDefaults(t *testing.T) {
	t.Run("zero-value config gets 30s/80px defaults", func(t *testing.T) {
		interval, width := slideshowDefaults(SlideshowConfig{})
		if interval != 30*time.Second || width != 80 {
			t.Errorf("slideshowDefaults(zero) = (%v, %d), want (30s, 80)", interval, width)
		}
	})
	t.Run("configured values pass through unchanged", func(t *testing.T) {
		interval, width := slideshowDefaults(SlideshowConfig{Interval: Duration(5 * time.Second), ThumbWidth: 42})
		if interval != 5*time.Second || width != 42 {
			t.Errorf("slideshowDefaults(configured) = (%v, %d), want (5s, 42)", interval, width)
		}
	})
}
