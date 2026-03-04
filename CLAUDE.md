# Mural Digital

Simple digital signage player that cycles through images in a `content/` subdirectory. Optimized for Raspberry Pi.

## Tech Stack

- **Language:** Go
- **GUI:** Fyne v2
- **Build:** CGo required (GCC)
- **Tooling:** mise for Go version management

## Project Structure

- `main.go` — single-file application (slideshow logic, image loading, thumbnails)
- `content/` — runtime image directory (not committed; `.gitignore`d)
- `go.mod` / `go.sum` — Go module dependencies
- `mise.toml` — mise tool versions

## Build Notes

- First Fyne build on Windows is very slow (~20 min) due to CGo compilation. Subsequent builds use the cache.
- Use `go build -buildvcs=false .` if building from a repo with no commits.

## Go Best Practices

- Use `gofmt`/`goimports` for formatting — never manually style code.
- Handle all errors explicitly; never discard with `_`. Prefer `fmt.Errorf("context: %w", err)` for wrapping.
- Use `context.Context` for cancellation and timeouts in long-running or concurrent operations.
- Prefer returning errors over panicking. Reserve `panic` for truly unrecoverable states.
- Keep functions short and focused. If a function needs a comment explaining what it does, consider renaming it or splitting it.
- Use named return values sparingly — only when they improve clarity.
- Group imports in standard library, external, and internal blocks (enforced by `goimports`).
- Use `defer` for cleanup (closing files, unlocking mutexes) immediately after acquiring the resource.
- Prefer struct embedding over inheritance-style patterns.
- Use interfaces at the consumer side, not the producer side. Keep interfaces small (1-2 methods).
- Run `go vet` and `staticcheck` to catch common issues.

## Architecture Notes

- Images are loaded from `content/` subdirectory at runtime, sorted by filename.
- Tiny thumbnails (48px wide) are pre-loaded at startup for instant keyboard navigation.
- Full images are decoded and scaled to the window size on demand (`decodeAndFit`), never held at full resolution.
- A generation counter (`atomic.Int64`) prevents stale background loads from overwriting newer slides.
- All off-main-thread UI updates go through `fyne.Do()`.
- Supported formats: JPG, JPEG, PNG.

## Conventions

- Keep it simple — this is a single-purpose signage player.
- Target platform is Linux but we want to support Windows too.
- Images are loaded from `content/` subdirectory at runtime.
