package main

import (
	"context"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestParseProbeOutput exercises parseProbeOutput against the four real
// ffprobe 8.1.2 outputs transcribed in Architect.md -> ffprobe contract,
// plus hand-written edge cases for the remaining rejection rules.
func TestParseProbeOutput(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    videoInfo
		wantErr bool
	}{
		{
			name: "valid h264: stream duration wins over container duration",
			json: `{
    "programs": [],
    "stream_groups": [],
    "streams": [
        { "codec_name": "h264", "width": 640, "height": 480, "duration": "3.000000" }
    ],
    "format": { "duration": "5.000000" }
}`,
			want: videoInfo{duration: 3 * time.Second, width: 640, height: 480},
		},
		{
			name:    "audio-only file: no video stream",
			json:    `{ "programs": [], "stream_groups": [], "streams": [], "format": { "duration": "2.000000" } }`,
			wantErr: true,
		},
		{
			name: "hevc-in-mp4: rejected on codec",
			json: `{ "streams": [ { "codec_name": "hevc", "width": 320, "height": 240, "duration": "1.000000" } ],
  "format": { "duration": "1.000000" } }`,
			wantErr: true,
		},
		{
			name:    "corrupt file: streams and format both absent",
			json:    `{}`,
			wantErr: true,
		},
		{
			name:    "zero width",
			json:    `{ "streams": [ { "codec_name": "h264", "width": 0, "height": 480, "duration": "3.000000" } ] }`,
			wantErr: true,
		},
		{
			name:    "zero height",
			json:    `{ "streams": [ { "codec_name": "h264", "width": 640, "height": 0, "duration": "3.000000" } ] }`,
			wantErr: true,
		},
		{
			name:    "negative duration in both sources",
			json:    `{ "streams": [ { "codec_name": "h264", "width": 640, "height": 480, "duration": "-1.000000" } ], "format": { "duration": "-1.000000" } }`,
			wantErr: true,
		},
		{
			name: "unparseable stream duration falls back to format duration",
			json: `{ "streams": [ { "codec_name": "h264", "width": 640, "height": 480, "duration": "N/A" } ], "format": { "duration": "4.500000" } }`,
			want: videoInfo{duration: 4500 * time.Millisecond, width: 640, height: 480},
		},
		{
			name:    "unparseable duration in both sources",
			json:    `{ "streams": [ { "codec_name": "h264", "width": 640, "height": 480, "duration": "N/A" } ], "format": { "duration": "N/A" } }`,
			wantErr: true,
		},
		{
			name:    "malformed json never panics, returns an error",
			json:    `not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProbeOutput([]byte(tt.json))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseProbeOutput() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProbeOutput() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseProbeOutput() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFitDimensions(t *testing.T) {
	tests := []struct {
		name       string
		srcW, srcH int
		winW, winH float32
		wantW      int
		wantH      int
	}{
		{"landscape into portrait window", 1920, 1080, 480, 800, 480, 270},
		{"portrait into landscape window", 1080, 1920, 800, 480, 270, 480},
		{"square source", 500, 500, 800, 480, 480, 480},
		{"exact fit, already even", 640, 480, 640, 480, 640, 480},
		{"odd result rounds down to even", 641, 480, 641, 480, 640, 480},
		{"below minimum clamps to 2", 4000, 1, 1, 1000, 2, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotW, gotH := fitDimensions(tt.srcW, tt.srcH, tt.winW, tt.winH)
			if gotW != tt.wantW || gotH != tt.wantH {
				t.Errorf("fitDimensions(%d,%d,%v,%v) = (%d,%d), want (%d,%d)",
					tt.srcW, tt.srcH, tt.winW, tt.winH, gotW, gotH, tt.wantW, tt.wantH)
			}
			if gotW%2 != 0 || gotH%2 != 0 {
				t.Errorf("fitDimensions(%d,%d,%v,%v) = (%d,%d), want even dimensions",
					tt.srcW, tt.srcH, tt.winW, tt.winH, gotW, gotH)
			}
		})
	}
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found in PATH")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not found in PATH")
	}
}

// generateTestClip produces a 3s-video / 5s-audio divergent clip using the
// exact command validated during Phase 3's empirical verification pass —
// this is the subprocess-level regression fixture for blocker B2.
func generateTestClip(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "clip.mp4")
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi", "-i", "testsrc=size=640x480:rate=25:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=5",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generating test clip: %v\n%s", err, out)
	}
	return path
}

func generateAudioOnly(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "audio.m4a")
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:a", "aac",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generating audio-only fixture: %v\n%s", err, out)
	}
	return path
}

func writeCorruptMP4(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "corrupt.mp4")
	if err := os.WriteFile(path, []byte("this is not a video file"), 0o644); err != nil {
		t.Fatalf("writing corrupt fixture: %v", err)
	}
	return path
}

// TestVideoIntegration exercises probe/firstFrame/play against real ffmpeg
// output. Skips entirely when ffmpeg/ffprobe are not on PATH.
func TestVideoIntegration(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	clip := generateTestClip(t, dir)
	audioOnly := generateAudioOnly(t, dir)
	corrupt := writeCorruptMP4(t, dir)

	v := NewVideo()

	t.Run("probe returns the 3s stream duration, not the 5s container duration", func(t *testing.T) {
		info, err := v.probe(clip)
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		if info.duration < 2900*time.Millisecond || info.duration > 3100*time.Millisecond {
			t.Errorf("probe duration = %v, want ~3s", info.duration)
		}
	})

	t.Run("probe rejects an audio-only file", func(t *testing.T) {
		if _, err := v.probe(audioOnly); err == nil {
			t.Error("probe succeeded on an audio-only file, want error")
		}
	})

	t.Run("probe rejects a corrupt file", func(t *testing.T) {
		if _, err := v.probe(corrupt); err == nil {
			t.Error("probe succeeded on a corrupt file, want error")
		}
	})

	t.Run("firstFrame returns an image of the requested width", func(t *testing.T) {
		img, err := v.firstFrame(clip, 80)
		if err != nil {
			t.Fatalf("firstFrame: %v", err)
		}
		if got := img.Bounds().Dx(); got != 80 {
			t.Errorf("firstFrame width = %d, want 80", got)
		}
	})

	t.Run("play delivers frames of the requested size and count", func(t *testing.T) {
		const w, h = 320, 240
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var mu sync.Mutex
		var frameCount int
		var lastBounds image.Rectangle

		start := time.Now()
		done := make(chan error, 1)
		go v.play(ctx, clip, w, h,
			func(frame image.Image) {
				mu.Lock()
				frameCount++
				lastBounds = frame.Bounds()
				mu.Unlock()
			},
			func(err error) { done <- err },
		)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("play: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("play did not complete within 15s")
		}
		elapsed := time.Since(start)

		mu.Lock()
		defer mu.Unlock()
		if lastBounds.Dx() != w || lastBounds.Dy() != h {
			t.Errorf("frame bounds = %v, want %dx%d", lastBounds, w, h)
		}
		// 3s clip at 25fps, capped to min(30,25)=25fps -> ~75 frames.
		const wantFrames = 75
		if frameCount < wantFrames-5 || frameCount > wantFrames+5 {
			t.Errorf("frame count = %d, want ~%d", frameCount, wantFrames)
		}
		// -re paces playback at roughly real time; a clip that decodes in
		// well under its own duration indicates pacing regressed.
		if elapsed < 2*time.Second {
			t.Errorf("play finished in %v, want pacing close to the clip's duration", elapsed)
		}
	})

	t.Run("a cancelled context returns promptly and reaps the process", func(t *testing.T) {
		const w, h = 320, 240
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan error, 1)
		go v.play(ctx, clip, w, h, func(image.Image) {}, func(err error) { done <- err })

		// Let playback start (the 3s clip paced by -re is still running),
		// then cancel mid-stream.
		time.Sleep(300 * time.Millisecond)
		cancelledAt := time.Now()
		cancel()

		select {
		case <-done:
			if since := time.Since(cancelledAt); since > 2*time.Second {
				t.Errorf("play took %v to return after cancellation, want prompt", since)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("play did not return within 5s of cancellation; likely leaked the process")
		}
	})
}
