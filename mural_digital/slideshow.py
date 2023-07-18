import re
import tkinter
import typing as t
from unittest.mock import Mock

from PIL import Image, ImageTk
from mural_digital import CONTENT_PATH
from mural_digital.cron import StateChange, CronShim


class Slideshow:
    def __init__(self, cron: CronShim):
        self.window = tkinter.Tk()
        self.contents = sorted(CONTENT_PATH.glob("page*.png"))
        self.index = 0
        self.label: tkinter.Label = Mock()
        self.cron = cron

        self._bind_keyboard_mouse()
        self.window.after(37, self.check_cron)
        self.after = self.window.after(101, self.show_next)

        # These lines below are irrelevant in ratpoison, they are only to help testing
        self.window.title("Slideshow")
        self.window.geometry("1072x603")

    def check_cron(self):
        state_change = self.cron.check()
        if state_change == StateChange.turning_on:
            self.index = 0
        elif state_change == StateChange.turning_off:
            self.contents = sorted(CONTENT_PATH.glob("page*.png"))
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

    def _bind_keyboard_mouse(self):
        self.window.bind("<Control-c>", lambda _: self.window.destroy())
        self.window.bind("<Alt-F4>", lambda _: self.window.destroy())
        self.window.bind("<Button-1>", self.prev_slide)  # left mouse click
        self.window.bind("<Button-3>", self.next_slide)  # right mouse click
        self.window.bind("<Left>", self.prev_slide)
        self.window.bind("<Right>", self.next_slide)
        self.window.bind("<Up>", self.prev_slide)
        self.window.bind("<Down>", self.next_slide)
        self.window.bind("<space>", self.next_slide)
        self.window.bind("<Return>", self.next_slide)
        self.window.bind("<BackSpace>", self.prev_slide)
        self.window.bind("-", self.prev_slide)
        self.window.bind(",", self.prev_slide)
        self.window.bind(".", self.next_slide)
        self.window.bind("[", self.prev_slide)
        self.window.bind("]", self.next_slide)
        self.window.bind("<Prior>", self.prev_slide)  # page up
        self.window.bind("<Next>", self.next_slide)  # page down
        self.window.bind("<Home>", self.specific_slide(0))
        self.window.bind("<End>", self.specific_slide(-1))
        for number in range(10):
            self.window.bind(str(number), self.specific_slide((number - 1) % 10))

    def next_slide(self, _: tkinter.Event) -> None:
        self.window.after_cancel(self.after)
        self.show_next()

    def prev_slide(self, event: tkinter.Event) -> None:
        self.index -= 2
        self.next_slide(event)

    def specific_slide(self, number: int) -> t.Callable[[tkinter.Event], None]:
        def inner(event: tkinter.Event) -> None:
            self.index = number
            self.next_slide(event)

        return inner


if __name__ == "__main__":
    Slideshow(CronShim()).window.mainloop()
