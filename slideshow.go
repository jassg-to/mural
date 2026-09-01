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

// scanSlidePaths scans dir for images and returns a []Slide with no
// thumbnail decoded for any of them (thumb is always nil) — just the
// path/size/mtime identity a thumbnail or full decode is later keyed on.
// This is the fast path Run() uses at startup: discovering the file list
// this way, instead of through scanSlides, is what lets the first slide
// reach the screen without waiting for every other image in the directory
// to be thumbnail-decoded first. See startThumbnailLoad for how thumbnails
// get filled in afterward.
func scanSlidePaths(dir string) ([]Slide, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading content directory: %w", err)
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
		fi, err := e.Info()
		if err != nil {
			continue
		}
		slides = append(slides, Slide{
			path:  filepath.Join(dir, e.Name()),
			size:  fi.Size(),
			mtime: fi.ModTime(),
		})
	}
	return slides, nil
}

// scanSlides scans dir for images and returns a []Slide, with thumbnails
// decoded eagerly for every slide. existing slides whose path, size, and
// mtime are unchanged are reused as-is, without re-decoding a thumbnail.
//
// existing is a snapshot passed in by the caller and never mutated in
// place: scanSlides runs from Run() on the run-loop goroutine and from a
// reload's background goroutine, so a shared mutable map would be a
// concurrent map write — an unrecoverable Go runtime fatal, not a benign
// race.
//
// Used by Reload/ingest, where eagerly-decoded thumbnails are worth the
// wait since nothing is waiting on a first frame the way startup is;
// Run()'s own startup scan uses the cheaper scanSlidePaths instead.
func (s *Slideshow) scanSlides(existing []Slide, thumbWidth uint) ([]Slide, error) {
	bare, err := scanSlidePaths(s.dir)
	if err != nil {
		return nil, err
	}
	if len(bare) == 0 {
		return nil, nil
	}

	prev := make(map[string]Slide, len(existing))
	for _, sl := range existing {
		prev[sl.path] = sl
	}

	slides := make([]Slide, len(bare))
	for i, b := range bare {
		if old, ok := prev[b.path]; ok && old.size == b.size && old.mtime.Equal(b.mtime) {
			slides[i] = old
			continue
		}
		// A corrupt image is still included in the rotation with a nil
		// thumbnail rather than being excluded (loadThumbnail returns nil
		// silently on decode failure).
		slides[i] = Slide{
			path:  b.path,
			thumb: loadThumbnail(b.path, thumbWidth),
			size:  b.size,
			mtime: b.mtime,
		}
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
type reloadRequestMsg struct {
	wake bool
}
type reloadResultMsg struct {
	slides []Slide
	err    error
	wake   bool
}
type decodeResultMsg struct {
	gen int64
	img image.Image
}
type advanceMsg struct{}
type ingestCommitMsg struct{}
type ingestResultMsg struct {
	mountPoint string
	result     ingestResult
}
type thumbsLoadedMsg struct {
	slides []Slide
}

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

	cmdCh   chan any
	navCh   <-chan NavKey
	vtCh    <-chan vtEvent
	mediaCh <-chan string
	ctx     context.Context
	cancel  context.CancelFunc

	cec         *CEC
	startPaused bool

	// Ingest state, touched only on the run-loop goroutine.
	ingesting        bool     // an ingest goroutine is currently running
	ingestingMount   string   // the mount point that goroutine is processing
	pendingMounts    []string // queued mount points, deduped, capped at pendingMountsCap
	ingestCommitting bool     // true from ingestCommitMsg until the matching ingestResultMsg, so advanceMsg is a no-op while the commit renames files the run loop might otherwise try to show

	// ingestFn performs the whole stage-then-commit ingest transaction.
	// Defaulted to ingestVolume in NewSlideshow; overridable in tests.
	ingestFn func(ctx context.Context, mountPoint, contentDir string, avail func(string) (uint64, error), onCommit func()) ingestResult

	// onScheduleConfig, if set, is invoked with an accepted volume's
	// [schedule] settings. Assigned in main.go after NewSchedule returns
	// (a construction-order cycle prevents it from being a constructor
	// argument here); nil-checked before use since tests may not wire it.
	onScheduleConfig func(ScheduleConfig)
}

// pendingMountsCap bounds the queue of mount points waiting for the
// in-flight ingest to finish. A flaky USB port or re-enumerating hub could
// otherwise grow the queue without limit, re-running identical ingests for
// minutes after the operator walked away.
const pendingMountsCap = 8

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
		ingestFn:   ingestVolume,
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
	s.postCommand(reloadRequestMsg{wake: true})
}

// startRescan snapshots the current slides (unless freshThumbs forces
// every thumbnail to be re-decoded, e.g. because thumb_width may have
// changed) and rescans the content directory on a background goroutine,
// posting the result back into cmdCh. Must be called from the run-loop
// goroutine (it reads s.slides and s.thumbWidth directly).
//
// wake and freshThumbs are carried through to reloadResultMsg rather than
// acted on here, since the rescan itself runs on a background goroutine
// and only the result handler (on the run-loop goroutine) may call resume()
// or touch s.slides.
func (s *Slideshow) startRescan(wake, freshThumbs bool) {
	var existing []Slide
	if !freshThumbs {
		existing = make([]Slide, len(s.slides))
		copy(existing, s.slides)
	}
	thumbWidth := s.thumbWidth

	go func() {
		slides, err := s.scanSlides(existing, thumbWidth)
		select {
		case s.cmdCh <- reloadResultMsg{slides: slides, err: err, wake: wake}:
		case <-s.ctx.Done():
		}
	}()
}

// startThumbnailLoad decodes a thumbnail for every slide in a snapshot of
// s.slides on a background goroutine, then posts the results back into
// cmdCh. Must be called from the run-loop goroutine (it reads s.slides and
// s.thumbWidth directly).
//
// This exists only for Run()'s startup scan, which deliberately discovers
// slides via the thumbnail-free scanSlidePaths so the first slide can be
// decoded and presented without waiting on every other image in the
// directory first (see Run()'s doc comment). It runs exactly once, right
// after that startup scan; a later Reload or media ingest already gets
// eagerly-decoded thumbnails from scanSlides itself and has no need to call
// this again.
func (s *Slideshow) startThumbnailLoad() {
	if len(s.slides) == 0 {
		return
	}
	snapshot := make([]Slide, len(s.slides))
	copy(snapshot, s.slides)
	thumbWidth := s.thumbWidth

	go func() {
		for i := range snapshot {
			snapshot[i].thumb = loadThumbnail(snapshot[i].path, thumbWidth)
		}
		select {
		case s.cmdCh <- thumbsLoadedMsg{slides: snapshot}:
		case <-s.ctx.Done():
		}
	}()
}

// applyLoadedThumbs merges background-loaded thumbnails from
// startThumbnailLoad into s.slides. Must be called from the run-loop
// goroutine.
//
// Matching is by path/size/mtime rather than by index or a captured
// generation counter: if a Reload or media ingest replaced s.slides with a
// wholly different (or reordered, or resized) set while the background
// load was still running, a stale entry simply fails to match and is
// dropped silently — the replacement slide list already got its own
// correctly-decoded thumbnails from scanSlides. A slide that already
// acquired a thumbnail by some other means (e.g. the user navigated to it
// and it went through a full decode, though that path does not itself set
// thumb) is left untouched rather than overwritten, though in practice
// thumb is only ever set here or by scanSlides.
func (s *Slideshow) applyLoadedThumbs(loaded []Slide) {
	byPath := make(map[string]Slide, len(loaded))
	for _, sl := range loaded {
		byPath[sl.path] = sl
	}
	for i, cur := range s.slides {
		if cur.thumb != nil {
			continue
		}
		if sl, ok := byPath[cur.path]; ok && sl.size == cur.size && sl.mtime.Equal(cur.mtime) {
			s.slides[i].thumb = sl.thumb
		}
	}
}

// slideshowDefaults applies the zero-value defaults for slideshow
// settings — a 30-second interval and an 80px thumbnail width — to
// whichever fields of cfg are unset. Shared by startup (main.go) and live
// config application (the ingest result handler) so the two cannot drift
// apart.
func slideshowDefaults(cfg SlideshowConfig) (time.Duration, uint) {
	interval := time.Duration(cfg.Interval)
	if interval == 0 {
		interval = 30 * time.Second
	}
	thumbWidth := cfg.ThumbWidth
	if thumbWidth == 0 {
		thumbWidth = 80
	}
	return interval, thumbWidth
}

type applyConfigMsg struct {
	cfg SlideshowConfig
}

// ApplyConfig posts cfg to be applied on the run-loop goroutine. Safe to
// call from any goroutine except the run loop itself.
//
// cmdCh is unbuffered and the run loop is its only reader, so calling this
// from inside handleCommand — the run loop's own goroutine, which is
// exactly where the ingest-result handler runs — would be the run loop
// sending to itself with nothing selecting: a permanent freeze of the
// sign. The ingest-result handler must call applySlideshowConfig directly
// instead of this method.
func (s *Slideshow) ApplyConfig(cfg SlideshowConfig) {
	s.postCommand(applyConfigMsg{cfg: cfg})
}

// applySlideshowConfig assigns s.interval and s.thumbWidth from cfg,
// applying slideshowDefaults for any unset fields. Must only be called on
// the run-loop goroutine — see ApplyConfig's doc comment for why calling
// it from any other goroutine is unsafe here.
func (s *Slideshow) applySlideshowConfig(cfg SlideshowConfig) {
	s.interval, s.thumbWidth = slideshowDefaults(cfg)
}

// handleMediaMount is the run loop's mediaCh case, extracted into its own
// method so tests can drive the queue directly — mediaCh is read only
// inside Run's select, and Run cannot be called from a test. If no ingest
// is currently in flight, mp starts one immediately; otherwise mp is
// queued, deduplicated against both the in-flight mount and the existing
// queue, and dropped (with a warning) once the queue is at capacity.
// Dropping a duplicate is silent because the Analyst defines re-ingest as
// idempotent in result; dropping past the cap is a real lost event and
// must not be.
func (s *Slideshow) handleMediaMount(mp string) {
	if !s.ingesting {
		s.startIngest(mp)
		return
	}
	if mp == s.ingestingMount {
		return
	}
	for _, q := range s.pendingMounts {
		if q == mp {
			return
		}
	}
	if len(s.pendingMounts) >= pendingMountsCap {
		log.Printf("media: pending mount queue full (%d), dropping %s", pendingMountsCap, mp)
		return
	}
	s.pendingMounts = append(s.pendingMounts, mp)
}

// startIngest launches the ingest transaction for mp on a background
// goroutine — never on the run-loop goroutine, so the display keeps
// playing throughout a multi-megabyte copy — and posts its result back
// into cmdCh. Must be called from the run-loop goroutine (it sets
// s.ingesting/s.ingestingMount directly).
func (s *Slideshow) startIngest(mp string) {
	s.ingesting = true
	s.ingestingMount = mp
	go func() {
		res := s.ingestFn(s.ctx, mp, s.dir, availableBytes, func() {
			s.postCommand(ingestCommitMsg{})
		})
		s.postCommand(ingestResultMsg{mountPoint: mp, result: res})
	}()
}

// handleIngestResult processes one ingestResultMsg: clearing the ingesting
// flag and the commit barrier (on every disposition, including ignore and
// reject, so a bug in one path cannot strand the rotation frozen),
// starting the next queued mount point if any, and then dispatching on
// disposition. Must be called from the run-loop goroutine.
func (s *Slideshow) handleIngestResult(m ingestResultMsg) {
	s.ingesting = false
	s.ingestingMount = ""
	s.ingestCommitting = false

	if len(s.pendingMounts) > 0 {
		next := s.pendingMounts[0]
		s.pendingMounts = s.pendingMounts[1:]
		s.startIngest(next)
	}

	res := m.result
	switch {
	case res.disposition == volumeAccepted && res.err == nil:
		log.Printf("media: %s accepted (images applied: %v)", m.mountPoint, res.imagesApplied)
		// The direct, non-posting form: this handler runs on the run-loop
		// goroutine itself, and cmdCh is unbuffered — calling the exported
		// ApplyConfig here would be the run loop sending to itself with
		// nothing selecting, freezing the sign permanently.
		s.applySlideshowConfig(res.cfg.Slideshow)
		if s.onScheduleConfig != nil {
			s.onScheduleConfig(res.cfg.Schedule)
		}
		// freshThumbs=true: thumb_width may have just changed with the
		// adopted config, and scanSlides' reuse key has no notion of
		// thumbnail width.
		s.startRescan(true, true)
	case res.disposition == volumeIgnored:
		log.Printf("media: %s ignored (no config.toml present)", m.mountPoint)
	case res.disposition == volumeRejected:
		log.Printf("media: %s rejected: %v", m.mountPoint, res.err)
	default: // volumeAccepted && res.err != nil
		log.Printf("media: %s accepted but failed: %v", m.mountPoint, res.err)
		if res.mutated {
			// The commit renamed some active images into previous/ before
			// failing, so s.slides now names files that no longer exist.
			// Rescan to repair the in-memory rotation; wake=false keeps
			// "a failed ingest does not wake the display" intact.
			s.startRescan(false, true)
		}
	}
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
	if s.waiting() {
		return
	}
	n := len(s.slides)
	idx := (s.current + 1) % n
	s.show(idx, false)
}

// waiting reports whether the content directory currently holds no
// images. A waiting sign shows nothing, arms no advance timer, and starts
// no decode — but still runs normally, accepting a volume that may
// provision it. Must be called from the run-loop goroutine.
func (s *Slideshow) waiting() bool {
	return len(s.slides) == 0
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
	if s.waiting() {
		s.current = 0
		if err := s.renderer.Present(nil); err != nil {
			log.Printf("presenting blank frame (waiting for content): %v", err)
		}
		return
	}
	s.current = index
	gen := s.generation.Add(1)
	sl := s.slides[index]

	if instant {
		// NearestNeighbor, not CatmullRom: this thumbnail is a fleeting
		// placeholder, on screen only until the background decode below
		// finishes, and measured ~4.1x faster on the target hardware — see
		// compositeLetterboxed's doc comment.
		frame := compositeLetterboxed(sl.thumb, s.width, s.height, xdraw.NearestNeighbor)
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

	if s.waiting() {
		return
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
	case applyConfigMsg:
		s.applySlideshowConfig(m.cfg)
	case reloadRequestMsg:
		s.startRescan(m.wake, false)
	case reloadResultMsg:
		if m.err != nil {
			log.Printf("slideshow reload: %v", m.err)
			return
		}
		if len(m.slides) == 0 {
			log.Printf("slideshow reload: no images found in %s, waiting for content", s.dir)
		}
		s.slides = m.slides
		s.current = 0
		if m.wake {
			s.resume()
		}
	case decodeResultMsg:
		if s.generation.Load() != m.gen {
			return // stale — a newer show() has since superseded this decode
		}
		frame := compositeLetterboxed(m.img, s.width, s.height, xdraw.CatmullRom)
		if err := s.renderer.Present(frame); err != nil {
			log.Printf("presenting decoded frame: %v", err)
		}
	case advanceMsg:
		if s.paused || s.ingestCommitting {
			return
		}
		s.advanceToNext()
	case ingestCommitMsg:
		// The commit is about to start renaming active image files. An
		// advanceMsg landing in that window would decode a since-moved
		// file and present a solid black frame — the partial result the
		// Analyst forbids — so freeze the timer until the matching
		// ingestResultMsg re-arms it via rescan or repair.
		s.ingestCommitting = true
		if s.advanceTimer != nil {
			s.advanceTimer.Stop()
		}
	case ingestResultMsg:
		s.handleIngestResult(m)
	case thumbsLoadedMsg:
		s.applyLoadedThumbs(m.slides)
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
// shutdown; navEvents, vtEvents, and mediaEvents are optional (vtEvents may
// be nil when there is no VT to own, i.e. under HeadlessRenderer; mediaEvents
// may be nil when USB hotplug detection is disabled or unsupported) and are
// read directly in the run loop's select — a nil channel there simply never
// fires.
//
// Startup is deliberately two-phase for perceived performance: the initial
// scan (scanSlidePaths) only discovers file paths/size/mtime, decoding no
// thumbnails, so it stays fast regardless of how many images are in dir.
// show(0, false) then decodes and presents slide 0 at full resolution as
// soon as that one decode finishes — no thumbnail flash, since slide 0 has
// no thumbnail yet to flash. Thumbnails for every slide (including slide
// 0) are then generated in the background by startThumbnailLoad and merged
// into s.slides via cmdCh once ready, so instant-nav (NavLeft/Right/Home)
// keeps working exactly as before once that finishes. Navigating to a
// slide before its thumbnail has loaded falls back to compositeLetterboxed's
// existing nil-image handling — the same solid-black placeholder a corrupt
// image gets — until that slide's own full decode (kicked off by the same
// show() call) lands a moment later; this was chosen over inventing a new
// placeholder state because it reuses an already-correct, already-tested
// code path instead of adding one.
func (s *Slideshow) Run(cancel context.CancelFunc, navEvents <-chan NavKey, vtEvents <-chan vtEvent, mediaEvents <-chan string) error {
	slides, err := scanSlidePaths(s.dir)
	if err != nil {
		return err
	}
	if len(slides) == 0 {
		log.Printf("slideshow: no images found in %s, waiting for content", s.dir)
	}
	s.slides = slides
	s.cancel = cancel
	s.navCh = navEvents
	s.vtCh = vtEvents
	s.mediaCh = mediaEvents

	if s.startPaused {
		s.pause()
	} else {
		// instant=false, not true: slide 0 has no thumbnail yet (the
		// startup scan above never decodes one), so presenting "instantly"
		// here would only present a pointless black flash before the same
		// full decode lands a moment later. Skipping straight to the full
		// decode reaches real content just as fast, with no flash.
		s.show(0, false)
		go func() {
			if err := s.cec.TurnOn(); err != nil {
				log.Printf("CEC TurnOn (startup): %v", err)
			}
		}()
	}
	s.startThumbnailLoad()

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
		case mp := <-s.mediaCh:
			s.handleMediaMount(mp)
		}
	}
}
