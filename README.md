# Mural Digital

A simple digital signage player built with [Fyne](https://fyne.io/). Cycles through images from the current folder.

## Prerequisites

- Go 1.25+
- GCC (for CGo/Fyne) — on Windows, install via [MSYS2](https://www.msys2.org/) or [TDM-GCC](https://jmeubank.github.io/tdm-gcc/)

## Usage

```bash
go build .
./mural-digital
```

Place image files (PNG, JPG, etc.) in the same directory as the executable and run it.

## Development

```bash
go run .
```

> **Note:** The first build takes a long time (10-20 min on Windows) due to Fyne's CGo compilation. Subsequent builds are fast thanks to the build cache.
