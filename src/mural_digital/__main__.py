def main():
    from mural_digital.cron import CronWithHdmi
    from mural_digital.slideshow import Slideshow

    Slideshow(CronWithHdmi()).window.mainloop()
