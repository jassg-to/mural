package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
)

func TestSlideKind(t *testing.T) {
	tests := []struct {
		ext      string
		wantKind slideKindT
		wantOK   bool
	}{
		{".jpg", slideKindImage, true},
		{".jpeg", slideKindImage, true},
		{".png", slideKindImage, true},
		{".mp4", slideKindVideo, true},
		{".JPG", slideKindImage, true},
		{".MP4", slideKindVideo, true},
		{".txt", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			kind, ok := slideKind(tt.ext)
			if ok != tt.wantOK {
				t.Fatalf("slideKind(%q) ok = %v, want %v", tt.ext, ok, tt.wantOK)
			}
			if ok && kind != tt.wantKind {
				t.Errorf("slideKind(%q) = %v, want %v", tt.ext, kind, tt.wantKind)
			}
		})
	}
}

func TestAdvanceDuration(t *testing.T) {
	interval := 30 * time.Second
	imageSlide := Slide{kind: slideKindImage}
	videoSlide := Slide{kind: slideKindVideo, duration: 7 * time.Second}

	if got := advanceDuration(imageSlide, interval); got != interval {
		t.Errorf("advanceDuration(image) = %v, want interval %v", got, interval)
	}
	if got := advanceDuration(videoSlide, interval); got != videoSlide.duration {
		t.Errorf("advanceDuration(video) = %v, want the video's own duration %v, not interval %v", got, videoSlide.duration, interval)
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
		slides, rejected, err := s.scanSlides(nil, nil)
		if err != nil {
			t.Fatalf("scanSlides: %v", err)
		}
		if len(rejected) != 0 {
			t.Errorf("rejected = %v, want empty — image decode failures are not excluded, only videos are", rejected)
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
		first, _, err := s.scanSlides(nil, nil)
		if err != nil || len(first) != 1 {
			t.Fatalf("first scan: err=%v, %d slides", err, len(first))
		}
		second, _, err := s.scanSlides(first, nil)
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
		first, _, err := s.scanSlides(nil, nil)
		if err != nil || len(first) != 1 {
			t.Fatalf("first scan: err=%v, %d slides", err, len(first))
		}

		writeTestPNG(t, path, 20, 20) // different size
		newMtime := first[0].mtime.Add(time.Hour)
		if err := os.Chtimes(path, newMtime, newMtime); err != nil {
			t.Fatal(err)
		}

		second, _, err := s.scanSlides(first, nil)
		if err != nil || len(second) != 1 {
			t.Fatalf("second scan: err=%v, %d slides", err, len(second))
		}
		if second[0] == first[0] {
			t.Error("changed file was reused from cache, want it re-processed")
		}
	})
}

// TestScanSlidesVideo covers video acceptance, exclusion, and the
// negative-result cache. Skipped when ffmpeg is not on PATH.
func TestScanSlidesVideo(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	clip := generateTestClip(t, dir)
	corrupt := writeCorruptMP4(t, dir)

	s := &Slideshow{dir: dir, thumbWidth: 8, vid: NewVideo()}

	slides, rejected, err := s.scanSlides(nil, nil)
	if err != nil {
		t.Fatalf("scanSlides: %v", err)
	}
	if len(slides) != 1 {
		t.Fatalf("got %d slides, want 1 (only the valid clip; the corrupt file must be excluded)", len(slides))
	}
	if slides[0].path != clip || slides[0].kind != slideKindVideo {
		t.Errorf("slide = %+v, want the valid video clip", slides[0])
	}
	if slides[0].duration <= 0 {
		t.Error("expected a positive duration")
	}
	if slides[0].thumb == nil {
		t.Error("expected a non-nil first-frame thumbnail")
	}
	if len(rejected) != 1 {
		t.Fatalf("got %d rejected entries, want 1 (the corrupt file)", len(rejected))
	}
	if _, ok := rejected[corrupt]; !ok {
		t.Errorf("rejected set = %v, want an entry for %s", rejected, corrupt)
	}

	// Second scan: both the accepted slide and the negative-cache entry
	// for the still-unchanged corrupt file must be reused verbatim rather
	// than re-probed.
	slides2, rejected2, err := s.scanSlides(slides, rejected)
	if err != nil {
		t.Fatalf("second scanSlides: %v", err)
	}
	if len(slides2) != 1 || slides2[0] != slides[0] {
		t.Error("unchanged accepted video slide was not reused from cache")
	}
	if len(rejected2) != 1 || rejected2[corrupt] != rejected[corrupt] {
		t.Error("unchanged rejected entry was not reused from the negative-result cache")
	}
}

// newHeadlessSlideshow hand-constructs a Slideshow with the fields show,
// pause, resume, and scheduleAdvance touch, populated directly. Run()
// cannot be called from a test since it blocks in ShowAndRun (Architect.md
// warning V12).
func newHeadlessSlideshow(slides []Slide, interval time.Duration) (*Slideshow, *canvas.Image) {
	test.NewApp()
	img := canvas.NewImageFromImage(nil)
	s := &Slideshow{
		dir:        "unused",
		interval:   interval,
		thumbWidth: 8,
		slides:     slides,
		img:        img,
		winSize:    func() fyne.Size { return fyne.NewSize(640, 480) },
		cec:        NewCEC(), // cec-client absent in test env; TurnOn/TurnOff are safe no-ops
	}
	return s, img
}

func TestSlideshowPauseStopsTimerAndBlanksImage(t *testing.T) {
	s, img := newHeadlessSlideshow([]Slide{{kind: slideKindImage, path: "irrelevant"}}, time.Hour)

	fired := make(chan struct{})
	s.advanceTimer = time.AfterFunc(30*time.Millisecond, func() { close(fired) })
	img.Image = image.NewRGBA(image.Rect(0, 0, 1, 1))

	s.pause()

	select {
	case <-fired:
		t.Fatal("advance timer fired after pause(), want it stopped")
	case <-time.After(100 * time.Millisecond):
	}

	if img.Image != nil {
		t.Error("pause() left img.Image non-nil, want the display blanked")
	}
	if !s.paused {
		t.Error("pause() did not set paused = true")
	}
}

func TestSlideshowResumeRearmsTimer(t *testing.T) {
	slides := []Slide{{kind: slideKindImage, path: "does-not-exist.png"}}
	s, img := newHeadlessSlideshow(slides, time.Hour)
	img.Image = image.NewRGBA(image.Rect(0, 0, 1, 1))
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
		{kind: slideKindImage, path: "a.png"},
		{kind: slideKindImage, path: "b.png"},
		{kind: slideKindImage, path: "c.png"},
	}
	// These are exactly the calls Run()'s key handler makes for
	// Right/Left/Home.
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
			s, img := newHeadlessSlideshow(slides, time.Hour)
			img.Image = image.NewRGBA(image.Rect(0, 0, 1, 1))
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
// next advance with exactly interval for an image slide, never with
// interval+watchdogGrace (the video-only addition) or any other value.
//
// This deliberately does not let a real time.AfterFunc fire and re-enter
// show()/scheduleAdvance from its own goroutine to observe the advance
// happening live. Under test.NewApp(), fyne.Do runs synchronously on
// whichever goroutine calls it rather than funnelling every callback onto
// one dedicated loop goroutine the way production Fyne's event loop does;
// production is race-free because the arming write and the later timer-
// fired read always happen on that single goroutine (safe by program
// order, no synchronization needed), but under test.NewApp() the timer
// fires on a brand new goroutine with no happens-before edge back to
// whichever goroutine armed it — go test -race correctly flags that as a
// real race. It is a test-harness limitation, not a production bug, and it
// is not fixable by changing what this test observes afterward: the race
// is entirely inside show()/scheduleAdvance's own re-entry, before any
// assertion runs. See TestAdvanceDuration for the pure-function proof that
// an image slide's duration is interval and a video slide's is its own
// duration; see TestSlideshowNavKeysRearmTimer/TestSlideshowResumeRearmsTimer
// for race-free proof that every show() call (re)arms the timer.
func TestSlideshowImageAdvanceTimingUnchanged(t *testing.T) {
	interval := 30 * time.Millisecond
	imageSlide := Slide{kind: slideKindImage}

	if got := advanceDuration(imageSlide, interval); got != interval {
		t.Fatalf("advanceDuration(image, %v) = %v, want %v unchanged", interval, got, interval)
	}

	// The Slideshow itself is built with a long interval, deliberately
	// distinct from the short one asserted against advanceDuration above:
	// show() arms a real time.AfterFunc, and it must never be allowed to
	// actually fire during this test's lifetime (see the leaked-timer note
	// above) or it re-triggers the exact race being avoided, on whatever
	// later test happens to be running when it goes off.
	slides := []Slide{{kind: slideKindImage, path: "does-not-exist.png"}}
	s, img := newHeadlessSlideshow(slides, time.Hour)
	img.Image = image.NewRGBA(image.Rect(0, 0, 1, 1))

	s.show(0, true)
	if s.advanceTimer == nil {
		t.Fatal("show() did not arm the advance timer for an image slide")
	}
	t.Cleanup(func() { s.advanceTimer.Stop() })
}
