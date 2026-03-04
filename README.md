# Mural Digital

A simple digital signage player built with [Fyne](https://fyne.io/). Cycles through images in a `content/` subdirectory as a slideshow, optimized for low-memory devices like Raspberry Pi.

## Prerequisites

- Go 1.25+
- GCC (for CGo/Fyne) — on Windows, install via [MSYS2](https://www.msys2.org/) or [TDM-GCC](https://jmeubank.github.io/tdm-gcc/)

## Usage

```bash
go build .
mkdir -p content
# Place your .jpg / .jpeg / .png images in content/
./mural-digital
```

### Options

| Flag | Default | Description |
|------|---------|-------------|
| `-interval` | `30s` | Time between automatic slide transitions |

```bash
./mural-digital -interval 10s
```

### Controls

| Key | Action |
|-----|--------|
| Right arrow | Next slide |
| Left arrow | Previous slide |
| Home | First slide |
| Esc | Quit |

The window defaults to 800x450 and is resizable.

## How It Works

- Images are loaded from the `content/` directory in filename order.
- Tiny thumbnails are pre-loaded at startup for instant keyboard navigation.
- Full images are decoded and scaled to the window size on demand, keeping memory usage low.

## Development

```bash
go run .
```

> **Note:** The first build takes a long time (10-20 min on Windows) due to Fyne's CGo compilation. Subsequent builds are fast thanks to the build cache.
