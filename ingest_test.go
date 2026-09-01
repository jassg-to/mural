package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const usableConfigTOML = `
[schedule]
[schedule.monday]
all = ["08:00-20:00"]
`

const noOnWindowConfigTOML = `
[schedule]
[schedule.monday]
all = ["18:00-18:00"]
`

func TestClassifyVolume(t *testing.T) {
	t.Run("no config.toml is ignored with nil error", func(t *testing.T) {
		dir := t.TempDir()
		disp, cfg, err := classifyVolume(dir)
		if disp != volumeIgnored || cfg != nil || err != nil {
			t.Fatalf("got (%v, %v, %v), want (ignored, nil, nil)", disp, cfg, err)
		}
	})

	t.Run("unparseable config.toml is rejected", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("not [ valid toml"), 0o644); err != nil {
			t.Fatal(err)
		}
		disp, cfg, err := classifyVolume(dir)
		if disp != volumeRejected || cfg != nil || err == nil {
			t.Fatalf("got (%v, %v, %v), want (rejected, nil, non-nil)", disp, cfg, err)
		}
	})

	t.Run("config.toml with no on-window is rejected", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(noOnWindowConfigTOML), 0o644); err != nil {
			t.Fatal(err)
		}
		disp, cfg, err := classifyVolume(dir)
		if disp != volumeRejected || cfg != nil || err == nil {
			t.Fatalf("got (%v, %v, %v), want (rejected, nil, non-nil)", disp, cfg, err)
		}
	})

	t.Run("config.toml with an on-window is accepted", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(usableConfigTOML), 0o644); err != nil {
			t.Fatal(err)
		}
		disp, cfg, err := classifyVolume(dir)
		if disp != volumeAccepted || cfg == nil || err != nil {
			t.Fatalf("got (%v, %v, %v), want (accepted, non-nil, nil)", disp, cfg, err)
		}
	})

	t.Run("directory named config.toml is rejected", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "config.toml"), 0o755); err != nil {
			t.Fatal(err)
		}
		disp, cfg, err := classifyVolume(dir)
		if disp != volumeRejected || cfg != nil || err == nil {
			t.Fatalf("got (%v, %v, %v), want (rejected, nil, non-nil)", disp, cfg, err)
		}
	})

	t.Run("unreadable volume root is rejected, not ignored", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root: permission bits are not enforced")
		}
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o755) })

		disp, cfg, err := classifyVolume(dir)
		if disp != volumeRejected || cfg != nil || err == nil {
			t.Fatalf("got (%v, %v, %v), want (rejected, nil, non-nil)", disp, cfg, err)
		}
	})

	t.Run("differently-cased config with no exact config.toml is ignored", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Config.toml"), []byte(usableConfigTOML), 0o644); err != nil {
			t.Fatal(err)
		}
		disp, cfg, err := classifyVolume(dir)
		if disp != volumeIgnored || cfg != nil || err != nil {
			t.Fatalf("got (%v, %v, %v), want (ignored, nil, nil) — case-insensitive match must not change the disposition", disp, cfg, err)
		}
	})
}

// --- TestIngestVolume helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	if got := readFile(t, path); got != want {
		t.Errorf("%s content = %q, want %q", path, got, want)
	}
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s: want absent, got err=%v", path, err)
	}
}

// assertImageNames asserts dir's top-level images (per imageFilesIn) are
// exactly want, in any order.
func assertImageNames(t *testing.T, dir string, want []string) {
	t.Helper()
	files, _, err := imageFilesIn(dir)
	if err != nil {
		t.Fatalf("imageFilesIn(%s): %v", dir, err)
	}
	got := make(map[string]bool, len(files))
	for _, f := range files {
		got[f.name] = true
	}
	if len(got) != len(want) {
		t.Fatalf("%s images = %v, want %v", dir, files, want)
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("%s: missing expected image %q, got %v", dir, w, files)
		}
	}
}

type volumeSnapshotEntry struct {
	content []byte
	mtime   time.Time
}

// volumeSnapshot records every top-level entry's content and mtime, so a
// later call to assertVolumeUnchanged can confirm the ingest never wrote
// to, deleted from, or touched the volume's timestamps.
func volumeSnapshot(t *testing.T, dir string) map[string]volumeSnapshotEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	snap := make(map[string]volumeSnapshotEntry, len(entries))
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			t.Fatalf("stat %s: %v", e.Name(), err)
		}
		var content []byte
		if fi.Mode().IsRegular() {
			content, err = os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("reading %s: %v", e.Name(), err)
			}
		}
		snap[e.Name()] = volumeSnapshotEntry{content: content, mtime: fi.ModTime()}
	}
	return snap
}

func assertVolumeUnchanged(t *testing.T, dir string, want map[string]volumeSnapshotEntry) {
	t.Helper()
	got := volumeSnapshot(t, dir)
	if len(got) != len(want) {
		t.Fatalf("volume %s entry count changed: got %d, want %d", dir, len(got), len(want))
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Fatalf("volume %s: %s disappeared", dir, name)
			continue
		}
		if string(g.content) != string(w.content) {
			t.Errorf("volume %s: %s content changed", dir, name)
		}
		if !g.mtime.Equal(w.mtime) {
			t.Errorf("volume %s: %s mtime changed: got %v, want %v", dir, name, g.mtime, w.mtime)
		}
	}
}

func unlimitedAvail(string) (uint64, error) { return 1 << 40, nil }

func TestIngestVolume(t *testing.T) {
	t.Run("happy path: images and config replace the active set", func(t *testing.T) {
		content := t.TempDir()
		writeFile(t, filepath.Join(content, "old1.jpg"), "old-image-1")
		writeFile(t, filepath.Join(content, "old2.png"), "old-image-2")
		writeFile(t, filepath.Join(content, "config.toml"), "old-config")

		vol := t.TempDir()
		writeFile(t, filepath.Join(vol, "new1.jpg"), "new-image-1")
		writeFile(t, filepath.Join(vol, "new2.jpg"), "new-image-2")
		writeFile(t, filepath.Join(vol, "config.toml"), usableConfigTOML)
		snap := volumeSnapshot(t, vol)

		res := ingestVolume(context.Background(), vol, content, unlimitedAvail, nil)
		if res.disposition != volumeAccepted || res.err != nil || !res.imagesApplied || res.mutated {
			t.Fatalf("result = %+v, want accepted/nil-err/imagesApplied/not-mutated", res)
		}

		assertImageNames(t, content, []string{"new1.jpg", "new2.jpg"})
		assertFileContent(t, filepath.Join(content, "config.toml"), usableConfigTOML)
		assertImageNames(t, filepath.Join(content, previousDirName), []string{"old1.jpg", "old2.png"})
		assertFileContent(t, filepath.Join(content, previousDirName, "config.toml"), "old-config")
		assertAbsent(t, filepath.Join(content, stagingDirName))
		assertVolumeUnchanged(t, vol, snap)
	})

	t.Run("config-only volume leaves the rotation untouched", func(t *testing.T) {
		content := t.TempDir()
		writeFile(t, filepath.Join(content, "old1.jpg"), "old-image-1")
		writeFile(t, filepath.Join(content, "config.toml"), "old-config")

		vol := t.TempDir()
		writeFile(t, filepath.Join(vol, "config.toml"), usableConfigTOML)
		snap := volumeSnapshot(t, vol)

		res := ingestVolume(context.Background(), vol, content, unlimitedAvail, nil)
		if res.disposition != volumeAccepted || res.err != nil || res.imagesApplied || res.mutated {
			t.Fatalf("result = %+v, want accepted/nil-err/!imagesApplied/not-mutated", res)
		}

		assertImageNames(t, content, []string{"old1.jpg"})
		assertFileContent(t, filepath.Join(content, "config.toml"), usableConfigTOML)
		assertImageNames(t, filepath.Join(content, previousDirName), nil)
		assertFileContent(t, filepath.Join(content, previousDirName, "config.toml"), "old-config")
		assertAbsent(t, filepath.Join(content, stagingDirName))
		assertVolumeUnchanged(t, vol, snap)
	})

	t.Run("ignored volume changes nothing", func(t *testing.T) {
		content := t.TempDir()
		writeFile(t, filepath.Join(content, "old1.jpg"), "old-image-1")
		writeFile(t, filepath.Join(content, "config.toml"), "old-config")

		vol := t.TempDir()
		writeFile(t, filepath.Join(vol, "personal.jpg"), "someones-photo")
		snap := volumeSnapshot(t, vol)

		res := ingestVolume(context.Background(), vol, content, unlimitedAvail, nil)
		if res.disposition != volumeIgnored || res.err != nil {
			t.Fatalf("result = %+v, want ignored/nil-err", res)
		}
		assertImageNames(t, content, []string{"old1.jpg"})
		assertFileContent(t, filepath.Join(content, "config.toml"), "old-config")
		assertAbsent(t, filepath.Join(content, previousDirName))
		assertAbsent(t, filepath.Join(content, stagingDirName))
		assertVolumeUnchanged(t, vol, snap)
	})

	t.Run("rejected volume changes nothing", func(t *testing.T) {
		for _, tc := range []struct {
			name       string
			configBody string
		}{
			{"unparseable", "not [ valid toml"},
			{"no on-window", noOnWindowConfigTOML},
		} {
			t.Run(tc.name, func(t *testing.T) {
				content := t.TempDir()
				writeFile(t, filepath.Join(content, "old1.jpg"), "old-image-1")
				writeFile(t, filepath.Join(content, "config.toml"), "old-config")

				vol := t.TempDir()
				writeFile(t, filepath.Join(vol, "new1.jpg"), "new-image-1")
				writeFile(t, filepath.Join(vol, "config.toml"), tc.configBody)
				snap := volumeSnapshot(t, vol)

				res := ingestVolume(context.Background(), vol, content, unlimitedAvail, nil)
				if res.disposition != volumeRejected || res.err == nil {
					t.Fatalf("result = %+v, want rejected/non-nil-err", res)
				}
				assertImageNames(t, content, []string{"old1.jpg"})
				assertFileContent(t, filepath.Join(content, "config.toml"), "old-config")
				assertAbsent(t, filepath.Join(content, previousDirName))
				assertAbsent(t, filepath.Join(content, stagingDirName))
				assertVolumeUnchanged(t, vol, snap)
			})
		}
	})

	t.Run("insufficient space declines before touching anything", func(t *testing.T) {
		content := t.TempDir()
		writeFile(t, filepath.Join(content, "old1.jpg"), "old-image-1")
		writeFile(t, filepath.Join(content, "config.toml"), "old-config")

		vol := t.TempDir()
		writeFile(t, filepath.Join(vol, "new1.jpg"), "new-image-1")
		writeFile(t, filepath.Join(vol, "config.toml"), usableConfigTOML)
		snap := volumeSnapshot(t, vol)

		tinyAvail := func(string) (uint64, error) { return 1, nil }
		res := ingestVolume(context.Background(), vol, content, tinyAvail, nil)
		if res.disposition != volumeAccepted || res.err == nil || res.mutated {
			t.Fatalf("result = %+v, want accepted/non-nil-err/not-mutated", res)
		}
		assertImageNames(t, content, []string{"old1.jpg"})
		assertFileContent(t, filepath.Join(content, "config.toml"), "old-config")
		assertAbsent(t, filepath.Join(content, stagingDirName))
		assertVolumeUnchanged(t, vol, snap)
	})

	t.Run("mid-copy failure leaves the active set intact", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root: permission bits are not enforced")
		}
		content := t.TempDir()
		writeFile(t, filepath.Join(content, "old1.jpg"), "old-image-1")
		writeFile(t, filepath.Join(content, "config.toml"), "old-config")

		vol := t.TempDir()
		unreadable := filepath.Join(vol, "bad.jpg")
		writeFile(t, unreadable, "cannot read me")
		if err := os.Chmod(unreadable, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(unreadable, 0o644) })
		writeFile(t, filepath.Join(vol, "config.toml"), usableConfigTOML)

		res := ingestVolume(context.Background(), vol, content, unlimitedAvail, nil)
		if res.disposition != volumeAccepted || res.err == nil || res.mutated {
			t.Fatalf("result = %+v, want accepted/non-nil-err/not-mutated", res)
		}
		assertImageNames(t, content, []string{"old1.jpg"})
		assertFileContent(t, filepath.Join(content, "config.toml"), "old-config")
		assertAbsent(t, filepath.Join(content, stagingDirName))
	})

	t.Run("retention bound: two consecutive ingests keep only the second's displaced set", func(t *testing.T) {
		content := t.TempDir()
		writeFile(t, filepath.Join(content, "orig.jpg"), "original-image")
		writeFile(t, filepath.Join(content, "config.toml"), "orig-config")

		volA := t.TempDir()
		writeFile(t, filepath.Join(volA, "a1.jpg"), "a1")
		writeFile(t, filepath.Join(volA, "a2.jpg"), "a2")
		writeFile(t, filepath.Join(volA, "config.toml"), usableConfigTOML)
		if res := ingestVolume(context.Background(), volA, content, unlimitedAvail, nil); res.err != nil {
			t.Fatalf("first ingest: %+v", res)
		}

		volB := t.TempDir()
		writeFile(t, filepath.Join(volB, "b1.jpg"), "b1")
		writeFile(t, filepath.Join(volB, "config.toml"), usableConfigTOML)
		if res := ingestVolume(context.Background(), volB, content, unlimitedAvail, nil); res.err != nil {
			t.Fatalf("second ingest: %+v", res)
		}

		assertImageNames(t, content, []string{"b1.jpg"})
		assertImageNames(t, filepath.Join(content, previousDirName), []string{"a1.jpg", "a2.jpg"})
		assertFileContent(t, filepath.Join(content, previousDirName, "config.toml"), usableConfigTOML)
	})

	t.Run("config-only retention: a settings-only stick must not destroy the image recovery copy", func(t *testing.T) {
		content := t.TempDir()
		writeFile(t, filepath.Join(content, "orig.jpg"), "original-image")
		writeFile(t, filepath.Join(content, "config.toml"), "orig-config")

		volImages := t.TempDir()
		writeFile(t, filepath.Join(volImages, "img1.jpg"), "img1")
		writeFile(t, filepath.Join(volImages, "config.toml"), usableConfigTOML)
		if res := ingestVolume(context.Background(), volImages, content, unlimitedAvail, nil); res.err != nil {
			t.Fatalf("image ingest: %+v", res)
		}
		assertImageNames(t, filepath.Join(content, previousDirName), []string{"orig.jpg"})

		volConfigOnly := t.TempDir()
		const secondConfig = "\n[schedule]\n[schedule.tuesday]\nall = [\"09:00-17:00\"]\n"
		writeFile(t, filepath.Join(volConfigOnly, "config.toml"), secondConfig)
		if res := ingestVolume(context.Background(), volConfigOnly, content, unlimitedAvail, nil); res.err != nil || res.imagesApplied {
			t.Fatalf("config-only ingest: %+v", res)
		}

		// previous/ still holds the FIRST stick's displaced images — a
		// settings-only stick displaces no images and must not touch them.
		assertImageNames(t, filepath.Join(content, previousDirName), []string{"orig.jpg"})
		// but previous/config.toml now holds what was active just before
		// the config-only ingest: the first stick's config.
		assertFileContent(t, filepath.Join(content, previousDirName, "config.toml"), usableConfigTOML)
		assertImageNames(t, content, []string{"img1.jpg"})
		assertFileContent(t, filepath.Join(content, "config.toml"), secondConfig)
	})

	t.Run("symlinks and FIFOs are skipped, not copied or hung on", func(t *testing.T) {
		content := t.TempDir()
		writeFile(t, filepath.Join(content, "config.toml"), "old-config")

		vol := t.TempDir()
		writeFile(t, filepath.Join(vol, "real.jpg"), "real-image")
		if err := os.Symlink("/nonexistent-target", filepath.Join(vol, "link.jpg")); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(filepath.Join(vol, "pipe.jpg"), 0o644); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(vol, "config.toml"), usableConfigTOML)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		res := ingestVolume(ctx, vol, content, unlimitedAvail, nil)
		if res.disposition != volumeAccepted || res.err != nil {
			t.Fatalf("result = %+v, want accepted/nil-err", res)
		}
		assertImageNames(t, content, []string{"real.jpg"})
	})

	t.Run("content/config.toml is never observably absent across the commit", func(t *testing.T) {
		content := t.TempDir()
		writeFile(t, filepath.Join(content, "old1.jpg"), "old-image-1")
		// Must be valid TOML, unlike other subtests' placeholder text: this
		// one actually parses it on every concurrent read to detect a
		// window where the rename target is missing or half-written.
		writeFile(t, filepath.Join(content, "config.toml"), "[schedule]\n")

		vol := t.TempDir()
		writeFile(t, filepath.Join(vol, "new1.jpg"), "new-image-1")
		writeFile(t, filepath.Join(vol, "config.toml"), usableConfigTOML)

		configPath := filepath.Join(content, "config.toml")
		stop := make(chan struct{})
		errCh := make(chan error, 1)
		go func() {
			for {
				select {
				case <-stop:
					errCh <- nil
					return
				default:
				}
				if _, err := LoadConfig(configPath); err != nil {
					errCh <- fmt.Errorf("content/config.toml unreadable mid-ingest: %w", err)
					return
				}
			}
		}()

		res := ingestVolume(context.Background(), vol, content, unlimitedAvail, nil)
		close(stop)
		if err := <-errCh; err != nil {
			t.Error(err)
		}
		if res.err != nil {
			t.Fatalf("ingest failed: %+v", res)
		}
	})
}
