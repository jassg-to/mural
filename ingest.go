package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// stagingDirName and previousDirName are the two directories an ingest
// maintains inside the content directory. Both are skipped by scanSlides'
// existing directory filter, so neither needs a scanner change. See
// Architect.md "Ingest is a stage-then-commit transaction".
const (
	stagingDirName  = ".ingest-staging"
	previousDirName = "previous"
)

func stagingPath(contentDir string) string  { return filepath.Join(contentDir, stagingDirName) }
func previousPath(contentDir string) string { return filepath.Join(contentDir, previousDirName) }

// ingestFreeSpaceMargin is kept free beyond the incoming payload's size so
// a successful ingest can never leave the SD card at literally zero free
// bytes — which would break the next ingest, the log, and Samba
// simultaneously.
const ingestFreeSpaceMargin = 32 << 20 // 32 MiB

// availableBytes returns the free space available to an unprivileged
// process on the filesystem containing dir.
//
// Both uint64 conversions are required: unix.Statfs_t.Bsize is int32 on
// GOARCH=arm and int64 on arm64/amd64, and armv7 is a published release
// target, so an unconverted multiply compiles cleanly on a development
// machine and fails only in CI on tag push.
func availableBytes(dir string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", dir, err)
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

// volumeDisposition is the outcome of classifying a mounted volume against
// the "config.toml at the top level" marker. See Architect.md "A volume
// has exactly three dispositions, and they are distinct."
type volumeDisposition int

const (
	// volumeIgnored: no config.toml at the volume's top level. The volume
	// is not addressed to Mural — a personal flash drive, a camera card, a
	// phone charging. Not logged as a failure.
	volumeIgnored volumeDisposition = iota
	// volumeRejected: a config.toml is present but unusable (unreadable,
	// unparseable, or defines no on-window). The volume is addressed to
	// Mural and is broken. Logged as an error.
	volumeRejected
	// volumeAccepted: a usable config.toml is present. The ingest proceeds.
	volumeAccepted
)

func (d volumeDisposition) String() string {
	switch d {
	case volumeIgnored:
		return "ignored"
	case volumeRejected:
		return "rejected"
	case volumeAccepted:
		return "accepted"
	default:
		return "unknown"
	}
}

// classifyVolume determines mountPoint's disposition by looking for
// config.toml at its top level.
//
// The stat of <mountPoint>/config.toml has three outcomes, not two:
//   - fs.ErrNotExist: volumeIgnored, nil config, nil error. This is the
//     expected outcome for a personal flash drive and must not be logged as
//     a failure.
//   - any other stat error (EACCES on the volume root, EIO from a corrupt
//     filesystem, the mount vanishing mid-classify): volumeRejected with
//     that error wrapped. Conflating this with "absent" would silently
//     swallow the Analyst's "volume mounts but is unreadable → ingest
//     failure, logged" case into the silent ignored disposition.
//   - stat succeeds but the entry is not a regular file (e.g. a directory
//     named config.toml): volumeRejected with a descriptive error — the
//     name is present, so the volume is addressed to Mural and is broken.
//
// Beyond that: LoadConfig failing is volumeRejected with the parse error;
// hasAnyOnWindow(cfg.Schedule) being false is volumeRejected with a
// descriptive "no on-window" error; otherwise volumeAccepted with the
// parsed config.
//
// The name is matched case-sensitively as exactly "config.toml". On the
// ignored path only, the volume's top level is additionally scanned for a
// case-insensitive match (e.g. "Config.toml"), and if one exists it is
// logged at info level — ignored is the silent disposition and therefore
// the hardest to diagnose, and this turns "a Windows-prepared stick does
// nothing" from a mystery into a readable answer.
func classifyVolume(mountPoint string) (volumeDisposition, *Config, error) {
	configPath := filepath.Join(mountPoint, "config.toml")

	fi, err := os.Stat(configPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			logCaseInsensitiveConfig(mountPoint)
			return volumeIgnored, nil, nil
		}
		return volumeRejected, nil, fmt.Errorf("statting %s: %w", configPath, err)
	}
	if !fi.Mode().IsRegular() {
		return volumeRejected, nil, fmt.Errorf("%s exists but is not a regular file", configPath)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		return volumeRejected, nil, fmt.Errorf("loading %s: %w", configPath, err)
	}
	if !hasAnyOnWindow(cfg.Schedule) {
		return volumeRejected, nil, fmt.Errorf("%s defines no on-window anywhere; adopting it would leave the sign permanently off", configPath)
	}

	return volumeAccepted, cfg, nil
}

// volumeFile is one enumerated top-level image: just enough to size and
// name the copy, not the full os.FileInfo interface, which carries no
// useful extra information here.
type volumeFile struct {
	name string
	size int64
}

// imageFilesIn enumerates dir's top level (no recursion, matching the
// content directory's existing flat treatment) and returns every entry
// that is a supported image. This is the single definition of "the images
// in a directory", used identically for the volume, content/, and
// previous/ — if each grew its own ad-hoc predicate, the results would
// diverge, and Step 12's commit depends on this enumeration agreeing
// exactly with what scanSlides considers an image (slideshow.go:95-102),
// or content/config.toml would be swept along with the images.
//
// Each entry's info comes from d.Info(), which has lstat semantics and
// does not follow symlinks — so a symlink named big.jpg pointing outside
// the volume is skipped rather than copied. Only regular files pass: a
// FIFO or device node named x.jpg would otherwise hang io.Copy forever on
// the background goroutine, with the sign still running and no
// indication why the ingest never completes.
func imageFilesIn(dir string) ([]volumeFile, int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("reading %s: %w", dir, err)
	}

	var files []volumeFile
	var total int64
	for _, e := range entries {
		if !isImageExt(filepath.Ext(e.Name())) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if !fi.Mode().IsRegular() {
			continue
		}
		files = append(files, volumeFile{name: filepath.Base(e.Name()), size: fi.Size()})
		total += fi.Size()
	}
	return files, total, nil
}

// volumeImages enumerates mountPoint's top-level supported images. It is
// just a call to imageFilesIn, kept as a distinct name for readability at
// the call site.
func volumeImages(mountPoint string) ([]volumeFile, int64, error) {
	return imageFilesIn(mountPoint)
}

// stageVolume copies images and the volume's config.toml from mountPoint
// into a fresh .ingest-staging/ directory inside contentDir, opening every
// source read-only and never writing to the volume. Any error — including
// ctx cancellation checked between files, so a shutdown mid-copy does not
// leave scratch behind — removes the staging directory and returns the
// failure, leaving nothing else on the device touched.
func stageVolume(ctx context.Context, mountPoint string, images []volumeFile, contentDir string) error {
	staging := stagingPath(contentDir)
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("removing stale staging directory: %w", err)
	}
	if err := os.Mkdir(staging, 0o755); err != nil {
		return fmt.Errorf("creating staging directory: %w", err)
	}

	fail := func(cause error) error {
		if rmErr := os.RemoveAll(staging); rmErr != nil {
			log.Printf("ingest: removing staging directory after failure: %v", rmErr)
		}
		return cause
	}

	for _, img := range images {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if err := copyFileSynced(filepath.Join(mountPoint, img.name), filepath.Join(staging, img.name)); err != nil {
			return fail(fmt.Errorf("copying %s: %w", img.name, err))
		}
	}

	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := copyFileSynced(filepath.Join(mountPoint, "config.toml"), filepath.Join(staging, "config.toml")); err != nil {
		return fail(fmt.Errorf("copying config.toml: %w", err))
	}

	if err := fsyncDir(staging); err != nil {
		return fail(fmt.Errorf("fsyncing staging directory: %w", err))
	}
	return nil
}

// copyFileSynced copies src to dst, fsyncing dst and verifying the copied
// byte count against src's stat size — catching a yanked stick's EIO or a
// short read that io.Copy alone would not surface as a length mismatch.
func copyFileSynced(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	fi, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	n, err := io.Copy(out, in)
	if err != nil {
		out.Close()
		return err
	}
	if n != fi.Size() {
		out.Close()
		return fmt.Errorf("short copy: wrote %d bytes, source is %d", n, fi.Size())
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// fsyncDir fsyncs a directory's entry, which is what makes the files
// renamed/created inside it durable, not just the files themselves.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// ingestResult is the outcome of one call to ingestVolume.
type ingestResult struct {
	disposition   volumeDisposition
	cfg           *Config
	imagesApplied bool
	// mutated is true only for a commit that failed after its first
	// successful rename — it tells slideshow.go the in-memory rotation no
	// longer matches disk and must be re-scanned even though the ingest
	// failed. False everywhere else, including a step 3 reclaim failure:
	// that touches only previous/, never the active rotation, so "leave
	// the running player exactly as it was" still holds.
	mutated bool
	err     error
}

// ingestVolume classifies mountPoint and, if accepted, performs the whole
// stage-then-commit ingest transaction against contentDir. onCommit is
// invoked exactly once, immediately before the commit begins, and only if
// the ingest gets that far — it is how the run loop learns to raise its
// blank-prevention barrier before the active content directory starts
// changing. avail is the free-space query, injected so tests can supply a
// constrained value without needing a small filesystem.
func ingestVolume(ctx context.Context, mountPoint, contentDir string, avail func(string) (uint64, error), onCommit func()) ingestResult {
	disp, cfg, err := classifyVolume(mountPoint)
	if disp != volumeAccepted {
		return ingestResult{disposition: disp, cfg: cfg, err: err}
	}

	images, imagesSize, err := volumeImages(mountPoint)
	if err != nil {
		return ingestResult{disposition: volumeAccepted, cfg: cfg, err: fmt.Errorf("enumerating volume images: %w", err)}
	}
	hasImages := len(images) > 0

	// Reclaim the retention slot, scoped to the payload being displaced.
	// Doing this before the free-space gate is what keeps the gate honest:
	// space held by a set the retention bound already sanctions discarding
	// is space this ingest may use.
	if hasImages {
		if err := reclaimPreviousImages(contentDir); err != nil {
			return ingestResult{disposition: volumeAccepted, cfg: cfg, err: fmt.Errorf("reclaiming previous/ images: %w", err)}
		}
	}

	configSize, err := fileSize(filepath.Join(mountPoint, "config.toml"))
	if err != nil {
		return ingestResult{disposition: volumeAccepted, cfg: cfg, err: fmt.Errorf("statting volume config.toml: %w", err)}
	}

	availBytes, err := avail(contentDir)
	if err != nil {
		return ingestResult{disposition: volumeAccepted, cfg: cfg, err: fmt.Errorf("checking free space: %w", err)}
	}
	needed := uint64(imagesSize) + uint64(configSize) + uint64(ingestFreeSpaceMargin)
	if needed > availBytes {
		return ingestResult{disposition: volumeAccepted, cfg: cfg, err: fmt.Errorf("insufficient free space: need %d bytes (incl. %d margin), have %d", needed, ingestFreeSpaceMargin, availBytes)}
	}

	if err := stageVolume(ctx, mountPoint, images, contentDir); err != nil {
		return ingestResult{disposition: volumeAccepted, cfg: cfg, err: fmt.Errorf("staging volume contents: %w", err)}
	}

	if onCommit != nil {
		onCommit()
	}

	mutated, err := commitIngest(contentDir, hasImages, images)
	if err != nil {
		return ingestResult{disposition: volumeAccepted, cfg: cfg, mutated: mutated, err: fmt.Errorf("committing ingest: %w", err)}
	}

	return ingestResult{disposition: volumeAccepted, cfg: cfg, imagesApplied: hasImages}
}

// reclaimPreviousImages deletes the image files inside contentDir's
// previous/, leaving previous/config.toml untouched — the commit replaces
// that separately. A previous/ that does not yet exist (the volume's first
// ever ingest) has nothing to reclaim and is not an error.
func reclaimPreviousImages(contentDir string) error {
	previous := previousPath(contentDir)
	images, _, err := imageFilesIn(previous)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, img := range images {
		if err := os.Remove(filepath.Join(previous, img.name)); err != nil {
			return fmt.Errorf("removing previous/%s: %w", img.name, err)
		}
	}
	return nil
}

// fileSize stats path and returns its size in bytes.
func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// commitIngest performs the only window in which the active content
// directory changes:
//
//	a. os.MkdirAll previous/ — never a blanket delete; the retention reclaim
//	   (step 13 in ingestVolume) has already emptied it of images if that
//	   was warranted.
//	b. Only when hasImages: rename every active top-level image out of
//	   content/ into previous/, then rename every staged image from
//	   .ingest-staging/ into content/. This is the entire config-only case
//	   — an accepted volume with no images must leave the rotation exactly
//	   as it was and must never empty it.
//	c. Always, atomically: link content/config.toml to previous/config.toml
//	   (after removing any existing one) and then rename the staged config
//	   over content/config.toml. Link-then-rename rather than
//	   rename-then-rename because the latter leaves a window in which
//	   content/config.toml does not exist, and Schedule's daily reload()
//	   reads exactly that path from another goroutine.
//	d. Remove the now-empty .ingest-staging/.
//
// staged is the list of images already copied into .ingest-staging/ by
// stageVolume (ignored when hasImages is false).
//
// mutated is true only when the active rotation no longer matches what is
// on disk and slideshow.go must rescan even though the ingest failed:
// that is the case when (b)'s image renames succeeded (the in-memory
// slide set now names files that moved into previous/) but a later step
// failed, or when (b) itself failed and its own rollback also failed.
// When (b) fails and its rollback succeeds, the active rotation was never
// actually left in a changed state, so mutated is false — matching the
// Analyst's "leave the running player in the state it was in before the
// volume was seen" exactly rather than settling for repair.
func commitIngest(contentDir string, hasImages bool, staged []volumeFile) (mutated bool, err error) {
	previous := previousPath(contentDir)
	staging := stagingPath(contentDir)

	if err := os.MkdirAll(previous, 0o755); err != nil {
		return false, fmt.Errorf("creating previous/: %w", err)
	}

	imagesCommitted := false
	if hasImages {
		m, err := commitImages(contentDir, staged)
		if err != nil {
			return m, err
		}
		imagesCommitted = true
	}

	if err := commitConfig(contentDir); err != nil {
		return imagesCommitted, fmt.Errorf("committing config: %w", err)
	}

	if err := os.Remove(staging); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("ingest: removing emptied staging directory: %v", err)
	}
	return false, nil
}

// commitImages moves contentDir's current top-level images into previous/
// and the staged images into contentDir. The set of images to move out is
// exactly imageFilesIn(contentDir) — never an ad-hoc "every non-directory
// entry", which would sweep config.toml along with them and break
// commitConfig's link-then-rename.
//
// If any rename fails, the images already relocated are renamed straight
// back before returning — they are on the same filesystem as their
// origin, so this is always possible barring a second failure. Only if
// that rollback itself fails does this report mutated=true.
func commitImages(contentDir string, staged []volumeFile) (mutated bool, err error) {
	previous := previousPath(contentDir)
	staging := stagingPath(contentDir)

	original, _, err := imageFilesIn(contentDir)
	if err != nil {
		return false, fmt.Errorf("enumerating active images: %w", err)
	}

	movedOut := make([]volumeFile, 0, len(original))
	for _, img := range original {
		if err := os.Rename(filepath.Join(contentDir, img.name), filepath.Join(previous, img.name)); err != nil {
			if rbErr := rollbackMovedOut(contentDir, previous, movedOut); rbErr != nil {
				return true, fmt.Errorf("moving %s to previous/: %w (rollback also failed: %v)", img.name, err, rbErr)
			}
			return false, fmt.Errorf("moving %s to previous/: %w", img.name, err)
		}
		movedOut = append(movedOut, img)
	}

	movedIn := make([]volumeFile, 0, len(staged))
	for _, img := range staged {
		if err := os.Rename(filepath.Join(staging, img.name), filepath.Join(contentDir, img.name)); err != nil {
			for _, mi := range movedIn {
				if rmErr := os.Remove(filepath.Join(contentDir, mi.name)); rmErr != nil {
					log.Printf("ingest: rollback: removing partially-committed %s: %v", mi.name, rmErr)
				}
			}
			if rbErr := rollbackMovedOut(contentDir, previous, movedOut); rbErr != nil {
				return true, fmt.Errorf("moving staged %s into content/: %w (rollback also failed: %v)", img.name, err, rbErr)
			}
			return false, fmt.Errorf("moving staged %s into content/: %w", img.name, err)
		}
		movedIn = append(movedIn, img)
	}

	return false, nil
}

// rollbackMovedOut renames every image in movedOut back from previous/
// into contentDir, attempting all of them even if one fails, and returns
// the first error encountered (nil if every rename succeeded).
func rollbackMovedOut(contentDir, previous string, movedOut []volumeFile) error {
	var firstErr error
	for _, img := range movedOut {
		if err := os.Rename(filepath.Join(previous, img.name), filepath.Join(contentDir, img.name)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// commitConfig atomically replaces contentDir's active config.toml with
// the staged one, first preserving the outgoing config in previous/ via a
// hardlink (falling back to a copy if Link fails, which it cannot on a
// single filesystem, but this fails closed rather than skipping the
// preservation step).
func commitConfig(contentDir string) error {
	previous := previousPath(contentDir)
	staging := stagingPath(contentDir)
	contentConfig := filepath.Join(contentDir, "config.toml")
	previousConfig := filepath.Join(previous, "config.toml")
	stagedConfig := filepath.Join(staging, "config.toml")

	if err := os.Remove(previousConfig); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing old previous/config.toml: %w", err)
	}
	if err := os.Link(contentConfig, previousConfig); err != nil {
		if copyErr := copyFileSynced(contentConfig, previousConfig); copyErr != nil {
			return fmt.Errorf("preserving outgoing config.toml (link: %v, copy fallback: %w)", err, copyErr)
		}
	}
	if err := os.Rename(stagedConfig, contentConfig); err != nil {
		return fmt.Errorf("renaming staged config.toml into content/: %w", err)
	}
	return nil
}

// logCaseInsensitiveConfig scans mountPoint's top level for an entry whose
// name matches "config.toml" case-insensitively but not exactly, and logs
// it at info level if found. Called only on the ignored path.
func logCaseInsensitiveConfig(mountPoint string) {
	entries, err := os.ReadDir(mountPoint)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() != "config.toml" && strings.EqualFold(e.Name(), "config.toml") {
			log.Printf("media: %s has %s but not config.toml (case-sensitive); ignoring the volume — rename it to config.toml exactly", mountPoint, e.Name())
			return
		}
	}
}
