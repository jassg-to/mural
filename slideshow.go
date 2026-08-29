package main

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	xdraw "golang.org/x/image/draw"
)

var imageExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
}

// isImageExt reports whether ext (case-insensitive) is a recognized image
// file extension.
func isImageExt(ext string) bool {
	return imageExts[strings.ToLower(ext)]
}

// Slide holds a content file's path along with its cached thumbnail and the
// file stats used to detect whether the file has changed since last load.
type Slide struct {
	path  string
	thumb image.Image
	size  int64
	mtime time.Time
}

// loadThumbnail decodes path and scales it to width, preserving aspect
// ratio. x/image/draw has no width-only auto-height convenience (unlike
// nfnt/resize's Resize(width, 0, ...)), so the target height is computed
// manually — the same scale-factor math decodeAndFit uses.
func loadThumbnail(path string, width uint) image.Image {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil
	}

	bounds := src.Bounds()
	imgW := float64(bounds.Dx())
	imgH := float64(bounds.Dy())
	if imgW <= 0 || imgH <= 0 {
		return nil
	}
	scale := float64(width) / imgW
	targetH := int(math.Round(imgH * scale))
	if targetH < 1 {
		targetH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, int(width), targetH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, xdraw.Src, nil)
	return dst
}

// scanSlides scans dir for images and returns a []Slide. existing slides
// whose path, size, and mtime are unchanged are reused as-is, without
// re-decoding a thumbnail.
//
// existing is a snapshot passed in by the caller and never mutated in
// place: scanSlides runs from Run() on the run-loop goroutine and from a
// reload's background goroutine, so a shared mutable map would be a
// concurrent map write — an unrecoverable Go runtime fatal, not a benign
// race.
func (s *Slideshow) scanSlides(existing []Slide) ([]Slide, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("reading content directory: %w", err)
	}

	prev := make(map[string]Slide, len(existing))
	for _, sl := range existing {
		prev[sl.path] = sl
	}

	var slides []Slide
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if !isImageExt(ext) {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		fi, err := e.Info()
		if err != nil {
			continue
		}
		size, mtime := fi.Size(), fi.ModTime()

		if old, ok := prev[path]; ok && old.size == size && old.mtime.Equal(mtime) {
			slides = append(slides, old)
			continue
		}

		// A corrupt image is still included in the rotation with a nil
		// thumbnail rather than being excluded (loadThumbnail returns nil
		// silently on decode failure).
		slides = append(slides, Slide{
			path:  path,
			thumb: loadThumbnail(path, s.thumbWidth),
			size:  size,
			mtime: mtime,
		})
	}
	return slides, nil
}

// decodeAndFit decodes path and scales it to fit within width×height,
// preserving aspect ratio. The result is not itself letterboxed onto a
// width×height canvas — compositeLetterboxed does that uniformly for every
// frame (this path, the thumbnail-instant path, and the nil/corrupt path)
// at Present time.
func decodeAndFit(path string, width, height int) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening image: %w", err)
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}

	bounds := src.Bounds()
	imgW := float64(bounds.Dx())
	imgH := float64(bounds.Dy())
	scale := math.Min(float64(width)/imgW, float64(height)/imgH)
	targetW := int(math.Round(imgW * scale))
	targetH := int(math.Round(imgH * scale))
	if targetW < 1 {
		targetW = 1
	}
	if targetH < 1 {
		targetH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, xdraw.Src, nil)
	return dst, nil
}

// Run-loop internal messages. Every mutation that used to be marshalled
// through fyne.Do — Pause/Reload from any goroutine, a completed
// background decode, the advance timer firing — is now a value posted into
// Slideshow.cmdCh and handled inside the single run-loop goroutine in Run.
type pauseMsg struct{}
type reloadRequestMsg struct{}
type reloadResultMsg struct {
	slides []Slide
	err    error
}
type decodeResultMsg struct {
	gen int64
	img image.Image
}
type advanceMsg struct{}

// Slideshow loads images from dir and displays them as a fullscreen
// slideshow via a Renderer.
type Slideshow struct {
	dir        string
	interval   time.Duration
	thumbWidth uint
	width      int
	height     int

	// fields below are set during Run and accessed only on the run-loop
	// goroutine, except via Pause/Reload which post into cmdCh instead of
	// mutating directly.
	slides       []Slide
	current      int
	paused       bool
	generation   atomic.Int64
	renderer     Renderer
	advanceTimer *time.Timer

	cmdCh  chan any
	navCh  <-chan NavKey
	vtCh   <-chan vtEvent
	ctx    context.Context
	cancel context.CancelFunc

	cec         *CEC
	startPaused bool
}

// NewSlideshow constructs a Slideshow. ctx is stored immediately (not
// deferred to Run) so Pause/Reload are safe to call from any goroutine —
// including schedule.go's callback goroutine — even before Run begins.
// width and height are the renderer's discovered display size, known once
// at startup and never re-queried.
func NewSlideshow(ctx context.Context, dir string, interval time.Duration, thumbWidth uint, cec *CEC, renderer Renderer, width, height int) *Slideshow {
	return &Slideshow{
		dir:        dir,
		interval:   interval,
		thumbWidth: thumbWidth,
		width:      width,
		height:     height,
		renderer:   renderer,
		cec:        cec,
		cmdCh:      make(chan any),
		ctx:        ctx,
	}
}

// postCommand sends msg into cmdCh for the run loop to process, or gives up
// if ctx is already done (e.g. the process is shutting down and nothing
// will ever read cmdCh again).
func (s *Slideshow) postCommand(msg any) {
	select {
	case s.cmdCh <- msg:
	case <-s.ctx.Done():
	}
}

// Pause stops the slideshow, blacks out the display, and turns off the
// connected display via CEC. Safe to call from any goroutine.
func (s *Slideshow) Pause() {
	s.postCommand(pauseMsg{})
}

// pause blacks out the display, stops the advance timer, and sends CEC
// standby. Must be called from the run-loop goroutine.
func (s *Slideshow) pause() {
	s.paused = true
	s.generation.Add(1) // cancel any in-flight background decode
	if s.advanceTimer != nil {
		s.advanceTimer.Stop()
	}
	if err := s.renderer.Present(nil); err != nil {
		log.Printf("presenting blank frame: %v", err)
	}
	go func() {
		if err := s.cec.TurnOff(); err != nil {
			log.Printf("CEC TurnOff: %v", err)
		}
	}()
}

// resume un-pauses, redisplays the current slide, and sends CEC power-on.
// Must be called from the run-loop goroutine.
func (s *Slideshow) resume() {
	s.paused = false
	s.show(s.current, true)
	go func() {
		if err := s.cec.TurnOn(); err != nil {
			log.Printf("CEC TurnOn: %v", err)
		}
	}()
}

// Reload requests a rescan of the content directory, a reset to slide 0,
// and an un-pause. Safe to call from any goroutine.
func (s *Slideshow) Reload() {
	s.postCommand(reloadRequestMsg{})
}

// startReload snapshots the current slides and rescans the content
// directory on a background goroutine, posting the result back into cmdCh.
// Must be called from the run-loop goroutine (it reads s.slides directly).
func (s *Slideshow) startReload() {
	existing := make([]Slide, len(s.slides))
	copy(existing, s.slides)

	go func() {
		slides, err := s.scanSlides(existing)
		select {
		case s.cmdCh <- reloadResultMsg{slides: slides, err: err}:
		case <-s.ctx.Done():
		}
	}()
}

// scheduleAdvance stops any pending advance timer and arms a new one that
// posts an advanceMsg after d elapses. Must be called from the run-loop
// goroutine; advanceTimer is only ever touched there, so it needs no lock.
func (s *Slideshow) scheduleAdvance(d time.Duration) {
	if s.advanceTimer != nil {
		s.advanceTimer.Stop()
	}
	s.advanceTimer = time.AfterFunc(d, func() {
		select {
		case s.cmdCh <- advanceMsg{}:
		case <-s.ctx.Done():
		}
	})
}

// advanceToNext shows the next slide (auto-advance, no thumbnail flash).
// Must be called from the run-loop goroutine.
func (s *Slideshow) advanceToNext() {
	n := len(s.slides)
	idx := (s.current + 1) % n
	s.show(idx, false)
}

// show displays the slide at index and arms its auto-advance. Must be
// called from the run-loop goroutine.
//
// instant=true presents the thumbnail (or black, if the slide is corrupt
// and has no thumbnail) immediately; instant=false swaps straight to the
// finished frame with no thumbnail flash (auto-advance) once decoding
// completes — preserving today's show-something-immediately behaviour in
// intent, now that decoding always happens off the run-loop goroutine.
func (s *Slideshow) show(index int, instant bool) {
	s.current = index
	gen := s.generation.Add(1)
	sl := s.slides[index]

	if instant {
		frame := compositeLetterboxed(sl.thumb, s.width, s.height)
		if err := s.renderer.Present(frame); err != nil {
			log.Printf("presenting slide %s: %v", sl.path, err)
		}
	}

	s.startImageDecode(sl.path, gen)
	s.scheduleAdvance(s.interval)
}

// startImageDecode decodes and fits path in the background, then posts the
// result into cmdCh for the run loop to present if gen is still current. A
// decode failure (including a corrupt source image) posts a nil image,
// which compositeLetterboxed renders as a solid black frame — Mural's own
// definition of what a corrupt slide looks like.
func (s *Slideshow) startImageDecode(path string, gen int64) {
	width, height := s.width, s.height
	go func() {
		if s.generation.Load() != gen {
			return
		}
		img, err := decodeAndFit(path, width, height)
		if err != nil {
			log.Printf("decoding %s: %v", path, err)
			img = nil
		}
		if s.generation.Load() != gen {
			return
		}
		select {
		case s.cmdCh <- decodeResultMsg{gen: gen, img: img}:
		case <-s.ctx.Done():
		}
	}()
}

// handleNavKey applies today's exact key precedence, ported directly from
// main.go's former SetOnTypedKey switch: NavQuit and NavSleep act
// unconditionally and are checked first, and neither counts as the key
// that resumes a paused sign. Every other key resumes a paused sign first,
// then (for NavLeft/NavRight/NavHome) performs its navigation.
func (s *Slideshow) handleNavKey(key NavKey) {
	switch key {
	case NavQuit:
		s.cancel()
		return
	case NavSleep:
		s.pause()
		return
	}

	if s.paused {
		s.resume()
	}

	n := len(s.slides)
	switch key {
	case NavRight:
		s.show((s.current+1)%n, true)
	case NavLeft:
		s.show((s.current-1+n)%n, true)
	case NavHome:
		s.show(0, true)
	}
}

// handleCommand applies one message posted into cmdCh.
func (s *Slideshow) handleCommand(msg any) {
	switch m := msg.(type) {
	case pauseMsg:
		s.pause()
	case reloadRequestMsg:
		s.startReload()
	case reloadResultMsg:
		if m.err != nil {
			log.Printf("slideshow reload: %v", m.err)
			return
		}
		if len(m.slides) == 0 {
			log.Printf("slideshow reload: no images found in %s", s.dir)
			return
		}
		s.slides = m.slides
		s.current = 0
		s.resume()
	case decodeResultMsg:
		if s.generation.Load() != m.gen {
			return // stale — a newer show() has since superseded this decode
		}
		frame := compositeLetterboxed(m.img, s.width, s.height)
		if err := s.renderer.Present(frame); err != nil {
			log.Printf("presenting decoded frame: %v", err)
		}
	case advanceMsg:
		if s.paused {
			return
		}
		s.advanceToNext()
	}
}

// handleVTEvent reacts to a VT switch reported by vt.go. On reacquire, the
// current slide is redrawn now that DRM master and the CRTC mode have been
// restored; on release, nothing else is needed — Present calls simply fail
// (loudly logged, like any other display error) until the VT is reacquired.
func (s *Slideshow) handleVTEvent(ev vtEvent) {
	if ev == vtEventAcquired {
		s.show(s.current, true)
	}
}

// Run scans the content directory and runs the slideshow until ctx (stored
// at construction) is done. cancel is invoked by NavQuit to trigger that
// shutdown; navEvents and vtEvents are optional (vtEvents may be nil when
// there is no VT to own, i.e. under HeadlessRenderer) and are read directly
// in the run loop's select — a nil channel there simply never fires.
func (s *Slideshow) Run(cancel context.CancelFunc, navEvents <-chan NavKey, vtEvents <-chan vtEvent) error {
	slides, err := s.scanSlides(nil)
	if err != nil {
		return err
	}
	if len(slides) == 0 {
		return fmt.Errorf("no images found in %s", s.dir)
	}
	s.slides = slides
	s.cancel = cancel
	s.navCh = navEvents
	s.vtCh = vtEvents

	if s.startPaused {
		s.pause()
	} else {
		s.show(0, true)
		go func() {
			if err := s.cec.TurnOn(); err != nil {
				log.Printf("CEC TurnOn (startup): %v", err)
			}
		}()
	}

	for {
		select {
		case <-s.ctx.Done():
			return nil
		case msg := <-s.cmdCh:
			s.handleCommand(msg)
		case key := <-s.navCh:
			s.handleNavKey(key)
		case ev, ok := <-s.vtCh:
			if !ok {
				s.vtCh = nil // closed: disable this case rather than busy-select on it
				continue
			}
			s.handleVTEvent(ev)
		}
	}
}
