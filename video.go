package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/nfnt/resize"
)

const (
	probeTimeout      = 10 * time.Second
	firstFrameTimeout = 15 * time.Second
	// watchdogGrace is added on top of a video slide's probed duration when
	// arming the advance timer, so a wedged ffmpeg cannot stall the rotation
	// forever. EOS reliably arrives early (see play), so in normal operation
	// this only ever fires on a genuine wedge.
	watchdogGrace = 2 * time.Second
)

// videoInfo is the scan-time metadata extracted from a video file by probe.
type videoInfo struct {
	duration time.Duration
	width    int
	height   int
}

// Video wraps the ffmpeg/ffprobe CLIs for scan-time metadata/thumbnail
// extraction and playback. Mirrors cec.go's pattern: probed once at
// construction, every operation becomes a graceful no-op/error when the
// tools are not on PATH.
type Video struct {
	available bool
}

// NewVideo probes for ffmpeg and ffprobe on PATH. It never returns nil and
// never errors; when either binary is missing it logs one line and returns
// a *Video whose methods all report the "disabled" error.
func NewVideo() *Video {
	_, ffmpegErr := exec.LookPath("ffmpeg")
	_, ffprobeErr := exec.LookPath("ffprobe")
	if ffmpegErr != nil || ffprobeErr != nil {
		log.Printf("ffmpeg/ffprobe not found in PATH; video playback disabled")
		return &Video{available: false}
	}
	return &Video{available: true}
}

// probeOutput mirrors the JSON shape emitted by:
//
//	ffprobe -v error -select_streams v:0 \
//	  -show_entries stream=codec_name,width,height,duration:format=duration \
//	  -of json <abs-path>
//
// Verified against ffmpeg/ffprobe 8.1.2. Extra top-level keys such as
// "programs" and "stream_groups" are present in 8.x output and are ignored
// by encoding/json. width/height decode as JSON numbers; codec_name and
// both duration fields decode as JSON strings.
type probeOutput struct {
	Streams []probeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

type probeStream struct {
	CodecName string `json:"codec_name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Duration  string `json:"duration"`
}

type probeFormat struct {
	Duration string `json:"duration"`
}

// parseProbeOutput is a pure function parsing ffprobe's JSON output into a
// videoInfo, or rejecting the file with an error. It must never panic: a
// corrupt file makes ffprobe print a bare "{}" (streams and format both
// absent), which decodes to zero-valued fields rather than nil pointers, so
// every field access below is safe.
//
// Duration is read from streams[0].duration first, falling back to
// format.duration only when the stream duration is absent or unparseable.
// format.duration is the container duration and includes any audio track;
// preferring it would advance a slide before the (audio-less, -an) video
// playback actually finishes.
func parseProbeOutput(data []byte) (videoInfo, error) {
	var out probeOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return videoInfo{}, fmt.Errorf("parsing ffprobe output: %w", err)
	}

	if len(out.Streams) == 0 {
		return videoInfo{}, fmt.Errorf("no video stream found")
	}
	stream := out.Streams[0]

	if stream.CodecName != "h264" {
		return videoInfo{}, fmt.Errorf("unsupported codec %q (only h264 is supported)", stream.CodecName)
	}
	if stream.Width <= 0 || stream.Height <= 0 {
		return videoInfo{}, fmt.Errorf("invalid video dimensions %dx%d", stream.Width, stream.Height)
	}

	seconds, err := parseDurationSeconds(stream.Duration)
	if err != nil || seconds <= 0 {
		seconds, err = parseDurationSeconds(out.Format.Duration)
		if err != nil || seconds <= 0 {
			return videoInfo{}, fmt.Errorf("no valid duration in stream or format metadata")
		}
	}

	return videoInfo{
		duration: time.Duration(seconds * float64(time.Second)),
		width:    stream.Width,
		height:   stream.Height,
	}, nil
}

func parseDurationSeconds(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing duration %q: %w", s, err)
	}
	return v, nil
}

// probe runs ffprobe against path and returns its validated metadata.
// Filenames come from a network-writable Samba share, so path is resolved
// with filepath.Abs before being handed to the subprocess — an absolute
// path can never be mistaken for a flag. ffmpeg has no "--" end-of-options
// terminator (ffmpeg -i -- file fails outright), so none is added.
//
// A non-zero ffprobe exit code is not the only rejection signal: an
// audio-only file and an HEVC-in-MP4 file both exit 0 and are rejected only
// once parseProbeOutput inspects the JSON. cmd.Output waits on the process
// implicitly, so probe never leaves a zombie.
func (v *Video) probe(path string) (videoInfo, error) {
	if !v.available {
		return videoInfo{}, fmt.Errorf("video support disabled: ffmpeg/ffprobe not found")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return videoInfo{}, fmt.Errorf("resolving path %q: %w", path, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name,width,height,duration:format=duration",
		"-of", "json",
		abs,
	)
	out, runErr := cmd.Output()

	info, parseErr := parseProbeOutput(out)
	if parseErr != nil {
		if runErr != nil {
			return videoInfo{}, fmt.Errorf("running ffprobe on %s: %w (%v)", abs, runErr, parseErr)
		}
		return videoInfo{}, fmt.Errorf("probing %s: %w", abs, parseErr)
	}
	return info, nil
}

// fitDimensions is a pure contain-fit calculation mirroring decodeAndFit's
// math.Min rule. It returns even dimensions with a minimum of 2 so the
// rawvideo frame size ffmpeg is asked to produce is exact and predictable.
func fitDimensions(srcW, srcH int, winW, winH float32) (int, int) {
	scale := math.Min(float64(winW)/float64(srcW), float64(winH)/float64(srcH))
	w := int(math.Round(float64(srcW) * scale))
	h := int(math.Round(float64(srcH) * scale))
	return evenAtLeast2(w), evenAtLeast2(h)
}

func evenAtLeast2(v int) int {
	if v%2 != 0 {
		v--
	}
	if v < 2 {
		return 2
	}
	return v
}

// firstFrame extracts a video's first frame, decodes it, and resizes it to
// width (aspect preserved) exactly as loadThumbnail does for images. It
// runs synchronously inside scanSlides, which runs inside Reload, which
// runs at schedule turn-on — so a bounded timeout keeps a wedged input from
// stalling the display coming on.
func (v *Video) firstFrame(path string, width uint) (image.Image, error) {
	if !v.available {
		return nil, fmt.Errorf("video support disabled: ffmpeg/ffprobe not found")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path %q: %w", path, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), firstFrameTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-v", "error",
		"-i", abs,
		"-frames:v", "1",
		"-f", "image2pipe",
		"-vcodec", "png",
		"-",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("extracting first frame from %s: %w", abs, err)
	}

	src, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("decoding first frame from %s: %w", abs, err)
	}
	return resize.Resize(width, 0, src, resize.Lanczos3), nil
}

// boundedWriter retains at most limit bytes and silently discards the rest,
// always reporting a full write. ffmpeg's stderr must be drained
// continuously (os/exec spawns a copier goroutine for any non-*os.File
// io.Writer) or it blocks once the 64 KB pipe fills, wedging playback for
// exactly the malformed inputs this code exists to survive.
type boundedWriter struct {
	buf   bytes.Buffer
	limit int
}

func (b *boundedWriter) Write(p []byte) (int, error) {
	if remaining := b.limit - b.buf.Len(); remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		b.buf.Write(p[:remaining])
	}
	return len(p), nil
}

func (b *boundedWriter) String() string {
	return b.buf.String()
}

// play blocks, streaming raw RGBA frames of exactly w*h*4 bytes from ffmpeg
// and invoking onFrame once per frame on the caller's goroutine. Playback
// is muted (-an), paced at the source's native rate (-re — without it a 3 s
// clip decodes in ~99 ms), and capped to 30 fps inside the filter graph via
// fps='min(30,source_fps)'. A bare fps=30 is a rate *converter*, not a cap,
// and would duplicate frames on sub-30fps sources, so the min() form is
// required.
//
// There is deliberately no timeout: clips are legitimately arbitrarily
// long, so ctx cancellation is the only stop signal (the caller's watchdog
// timer covers a wedged process). Cancelling ctx kills ffmpeg; play always
// calls cmd.Wait() before returning, on every path, so no zombie is left
// behind even though StdoutPipe does not wait implicitly.
//
// Frame buffers are recycled from a small fixed pool rather than allocated
// per frame (a fresh *image.RGBA per frame is ~250 MB/s of garbage at
// 1080p30). Go's image.RGBA is alpha-premultiplied while ffmpeg's rgba
// output is straight alpha; this is harmless here because video carries no
// alpha channel and ffmpeg always emits A=255.
func (v *Video) play(ctx context.Context, path string, w, h int, onFrame func(image.Image), onDone func(error)) {
	if !v.available {
		onDone(fmt.Errorf("video support disabled: ffmpeg/ffprobe not found"))
		return
	}
	if w <= 0 || h <= 0 {
		onDone(fmt.Errorf("invalid playback dimensions %dx%d", w, h))
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		onDone(fmt.Errorf("resolving path %q: %w", path, err))
		return
	}

	filter := fmt.Sprintf("scale=%d:%d,fps='min(30,source_fps)'", w, h)
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-v", "error",
		"-re",
		"-i", abs,
		"-an",
		"-vf", filter,
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"-",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		onDone(fmt.Errorf("creating ffmpeg stdout pipe: %w", err))
		return
	}
	stderrBuf := &boundedWriter{limit: 4096}
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		onDone(fmt.Errorf("starting ffmpeg: %w", err))
		return
	}

	frameSize := w * h * 4
	const bufCount = 3
	bufs := make([]*image.RGBA, bufCount)
	for i := range bufs {
		bufs[i] = &image.RGBA{
			Pix:    make([]byte, frameSize),
			Stride: w * 4,
			Rect:   image.Rect(0, 0, w, h),
		}
	}

	var readErr error
	for i := 0; ; i++ {
		buf := bufs[i%bufCount]
		if _, err := io.ReadFull(stdout, buf.Pix); err != nil {
			if err != io.EOF {
				// io.ErrUnexpectedEOF (a short/truncated final frame) and
				// any other read error are failures, not a clean end.
				readErr = fmt.Errorf("reading video frame: %w", err)
			}
			break
		}
		onFrame(buf)
	}

	// Must Wait() on every path so the child is reaped: a kiosk running
	// unattended for months at one video slide per interval would otherwise
	// accumulate a zombie per slide until the process table is exhausted.
	waitErr := cmd.Wait()

	switch {
	case readErr != nil:
		onDone(fmt.Errorf("%w (ffmpeg stderr: %s)", readErr, stderrBuf.String()))
	case waitErr != nil:
		onDone(fmt.Errorf("ffmpeg exited: %w (stderr: %s)", waitErr, stderrBuf.String()))
	default:
		onDone(nil)
	}
}
