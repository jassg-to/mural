import tkinter
import tkinter.scrolledtext
import webbrowser

import yaml


class Settings:
    def __init__(self):
        doc = yaml.load(open('settings.yaml'))
        self.run_board = doc['run board']
        self.schedule_sheet = doc['schedule']['sheet']
        self.schedule_poll_at = doc['schedule']['poll at']


class Window(tkinter.Tk):
    def __init__(self):
        super().__init__()
        self.settings = Config()
        self.geometry('300x300')
        self.title('Mural Digital')
        text = tkinter.scrolledtext.ScrolledText(self, height=1)
        text.pack(fill=tkinter.BOTH, expand=1)
        # text.insert(INSERT, 'text')
        b = tkinter.Button(self, text='Launch Mural', command=self.launch_board)
        b.pack(fill=tkinter.BOTH, expand=1)
        a = tkinter.Button(self, text='Edit Schedule', command=self.edit_schedule)
        a.pack(fill=tkinter.BOTH, expand=1)

    def launch_board(self):
        webbrowser.open(self.settings.run_board)

    def edit_schedule(self):
        webbrowser.open(self.settings.schedule_sheet)


if __name__ == '__main__':
    Window().mainloop()
