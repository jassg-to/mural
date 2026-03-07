package main

import (
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func main() {
	a := app.New()
	w := a.NewWindow("Mural Digital")

	w.SetContent(container.NewVBox(
		widget.NewLabel("Hello, Mural Digital!"),
		widget.NewButton("Quit", func() { a.Quit() }),
	))

	w.ShowAndRun()
}
