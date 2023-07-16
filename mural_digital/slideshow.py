import re
import tkinter
from unittest.mock import Mock

from PIL import Image, ImageTk
from mural_digital import CONTENT_PATH
from mural_digital.cron import StateChange, Cron


class Slideshow:
    def __init__(self, cron: Cron = None):
        self.window = tkinter.Tk()
        self.contents = sorted(CONTENT_PATH.glob("page*.png"))
        self.index = 0
        self.label: tkinter.Label = Mock()
        self.cron = cron

        self.window.after(37, self.check_cron if cron else Mock())
        self.window.after(101, self.show_next)

        # These lines below are irrelevant in ratpoison, they are only to help testing
        self.window.title("Slideshow")
        self.window.geometry("1072x603")

    def check_cron(self):
        match self.cron.check():
            case StateChange.turning_on:
                self.index = 0
            case StateChange.turning_off:
                self.contents = sorted(CONTENT_PATH.glob("page*.png"))
        self.window.after(773, self.check_cron)

    def show_next(self) -> None:
        image = ImageTk.PhotoImage(self.get_next_image_resized())
        new_label = tkinter.Label(self.window, image=image)
        new_label.image = image
        new_label.place(x=0, y=0)
        self.label.destroy()
        self.label = new_label
        self.window.after(59879, self.show_next)

    def get_next_image_resized(self) -> Image:
        image = Image.open(self.contents[self.index])
        self.index = (self.index + 1) % len(self.contents)

        matches = re.finditer(r"\d+", self.window.geometry())
        width, height, *_ = (int(m.group(0)) for m in matches)

        return image.resize((width, height))


if __name__ == "__main__":
    Slideshow(None).window.mainloop()
