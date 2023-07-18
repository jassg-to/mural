import re
import tkinter
import typing as t
from unittest.mock import Mock

from PIL import Image, ImageTk
from mural_digital import CONTENT_PATH
from mural_digital.cron import StateChange, CronShim


class Slideshow:
    def __init__(self, cron: CronShim):
        self.window = self._build_window()
        self.after = self.window.after(23, self.show_next)
        self.contents = sorted(CONTENT_PATH.glob("page*.png"))
        self.index = 0
        self.label: tkinter.Label = Mock()
        self.cron = cron

    def check_cron(self):
        state_change = self.cron.check()
        if state_change == StateChange.turning_on:
            self.specific_slide(0)()
        self.window.after(773, self.check_cron)

    def show_next(self) -> None:
        image = ImageTk.PhotoImage(self.get_next_image_resized())
        new_label = tkinter.Label(self.window, image=image)
        new_label.image = image
        new_label.place(x=0, y=0)
        self.label.destroy()
        self.label = new_label
        self.index += 1
        self.after = self.window.after(self.cron.options.slide_time_seconds * 1000, self.show_next)

    def get_next_image_resized(self) -> Image:
        matches = re.finditer(r"\d+", self.window.geometry())
        width, height, *_ = (int(m.group(0)) for m in matches)

        self.index %= len(self.contents)
        image = Image.open(self.contents[self.index])
        return image.resize((width, height))

    def _build_window(self) -> tkinter.Tk:
        window = tkinter.Tk()

        # Events
        window.configure(bg="black", cursor="none")
        window.after(10000, self.check_cron)

        # Test settings (overriden by ratpoison)
        window.title("Slideshow")
        window.geometry("1072x603")

        # Keyboard and mouse bindings
        window.bind("<Control-c>", lambda _: self.window.destroy())
        window.bind("<Alt-F4>", lambda _: self.window.destroy())
        window.bind("<Button-1>", self.prev_slide)  # left mouse click
        window.bind("<Button-3>", self.next_slide)  # right mouse click
        window.bind("<Left>", self.prev_slide)
        window.bind("<Right>", self.next_slide)
        window.bind("<Up>", self.prev_slide)
        window.bind("<Down>", self.next_slide)
        window.bind("<space>", self.next_slide)
        window.bind("<Return>", self.next_slide)
        window.bind("<BackSpace>", self.prev_slide)
        window.bind("-", self.prev_slide)
        window.bind(",", self.prev_slide)
        window.bind(".", self.next_slide)
        window.bind("[", self.prev_slide)
        window.bind("]", self.next_slide)
        window.bind("<Prior>", self.prev_slide)  # page up
        window.bind("<Next>", self.next_slide)  # page down
        window.bind("<Home>", self.specific_slide(0))
        window.bind("<End>", self.specific_slide(-1))
        for number in range(10):
            window.bind(str(number), self.specific_slide((number - 1) % 10))

        return window

    def next_slide(self, *_) -> None:
        self.window.after_cancel(self.after)
        self.show_next()

    def prev_slide(self, *_) -> None:
        self.index -= 2
        self.next_slide()

    def specific_slide(self, number: int) -> t.Callable[..., None]:
        def to_specific_slide(*_) -> None:
            self.index = number
            self.next_slide()

        return to_specific_slide


if __name__ == "__main__":
    Slideshow(CronShim()).window.mainloop()
