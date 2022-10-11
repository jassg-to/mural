#!/usr/bin/env python3
import argparse
import os.path
import pathlib
import re
import subprocess
import sys
import tempfile
import typing

import elevate
import requests
import yaml

elevate.elevate()
MY_PATH = os.path.realpath(__file__)
ARGS_REFRESH = ("/usr/bin/xdotool", "key", "ctrl+F5")
ARGS_CEC_CLIENT = ("/usr/bin/cec-client", "-s")


class Session:
    url = ""

    def main(self):
        command = self.parse_arguments()
        self.load_settings()
        command()

    def parse_arguments(self) -> typing.Callable:
        parser = argparse.ArgumentParser()
        parser.add_argument("command", help="One of: on, off, schedule")
        command = parser.parse_args().command
        function = getattr(self, f"run_command_{command}", None)
        if not function:
            print(f"Unknown command: {command}")
            sys.exit(1)
        return function

    def load_settings(self):
        self.url = pathlib.Path("/root/mural-digital.txt").read_text().strip()

    @staticmethod
    def run_command_on():
        # DISPLAY=:0 xdotool key 'ctrl+F5'
        # echo 'on 0' | cec-client -s
        subprocess.run(ARGS_CEC_CLIENT, input=b"on 0\n")
        subprocess.run(ARGS_REFRESH, env={**os.environ, "DISPLAY": ":0"})

    @staticmethod
    def run_command_off():
        # echo 'standby 0' | cec-client -s
        subprocess.run(ARGS_CEC_CLIENT, input=b"standby 0\n")

    def run_command_schedule(self):
        with tempfile.NamedTemporaryFile("w") as file:
            for weekday, hour, minute, command in self.get_schedule():
                file.write(f"{minute:2} {hour:2} * * {weekday:3}\t{MY_PATH} {command}\n")
            file.flush()
            subprocess.run(["/usr/bin/crontab", file.name])

    def get_schedule(self) -> typing.Iterable[str]:
        file_contents = requests.get(self.url).text
        schedule = yaml.safe_load(file_contents)

        for weekday, items in schedule.items():
            for item in items:

                # Try on-off range:
                match = re.match(r"^(\d+):(\d+)\s*-\s*(\d+):(\d+)$", item)
                if match:
                    yield weekday, match.group(1), match.group(2), "on"
                    yield weekday, match.group(3), match.group(4), "off"
                    continue

                # Try single event command
                match = re.match(r"(\D.+)\s+(\d+):(\d+)", item)
                if match:
                    yield weekday, match.group(2), match.group(3), match.group(1)
                    continue

                # Something went wrong!
                raise ValueError("Could not understand: " + item)


if __name__ == "__main__":
    Session().main()
