from subprocess import Popen
from tkinter import DISABLED, Button, Tk, BOTH, NORMAL, INSERT
from tkinter.scrolledtext import ScrolledText
from typing import Callable
from webbrowser import open_new_tab

from common import settings, ScheduleCalculator
from readxlsheet import get_schedule


class Window(Tk):
    def __init__(self):
        super().__init__()
        self.geometry('300x300')
        self.title('Mural Digital')
        self.text = ScrolledText(self, height=3)
        self.text.pack(fill=BOTH, expand=1)

        @self._add_button
        def launch_board():
            url = settings.run_board
            cmd = 'chromium-browser --incognito --kiosk'.split() + [url]
            Popen(cmd)  # Fire & Forget
            return 5000

        @self._add_button
        def edit_schedule():
            open_new_tab(settings.schedule_sheet)
            return 5000

        @self._add_button
        def read_schedule():
            self.schedule = get_schedule()
            self.calculate_schedule()
            return 0

        self.schedule_calculator = ScheduleCalculator(get_schedule)

    def _add_button(self, func: Callable[[], int]):
        def wrapper():
            button.config(state=DISABLED)
            wait = 0
            try:
                wait = func()
            finally:
                button.after(wait, lambda: button.config(state=NORMAL))

        text = func.__name__.replace('_', ' ').title()
        button = Button(self, text=text)
        button.config(command=wrapper)
        button.pack(fill=BOTH, expand=1)
        return wrapper

    def calculate_schedule(self):
        agenda, wait, actions = self.schedule_calculator.calculate()
        # TODO
        self.text.insert(INSERT, '\n'.join(agenda))
        self.after(wait, lambda: print(actions))


if __name__ == '__main__':
    Window().mainloop()
