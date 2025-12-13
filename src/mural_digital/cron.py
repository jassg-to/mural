import enum
import re
import subprocess
import sys
import typing as t
from bisect import bisect_right
from datetime import datetime, time

from ruamel.yaml import YAML

from mural_digital import CONTENT_PATH

ARGS_CEC_CLIENT = ("/usr/bin/cec-client", "-s")
CONFIG_YAML_PATH = CONTENT_PATH / "config.yaml"
yaml = YAML()


class StateChange(enum.IntEnum):
    no_change = 0
    turning_on = 1
    turning_off = 2


class Options(t.NamedTuple):
    slide_time_seconds: int = 30


class Cron:
    options: Options = Options()
    current_weekday: int = datetime.now().weekday()
    today_list: t.List[time] = []

    def __init__(self):
        self.read_config()
        self.state = True

    def read_config(self) -> None:
        with open(CONFIG_YAML_PATH) as f:
            raw_config = yaml.load(f)
        self.options = Options(**raw_config["options"])
        schedule = parse_schedule(raw_config["schedule"])
        self.today_list = schedule[self.current_weekday]

    def check(self):
        now = datetime.now()
        weekday = now.weekday()
        if weekday != self.current_weekday:
            sys.exit(0)  # restart to pick up updates

        now_time = now.time()
        current_spot = bisect_right(self.today_list, now_time)
        new_state = bool(current_spot % 2)
        if new_state == self.state:
            return StateChange.no_change
        self.state = new_state
        if new_state:
            return StateChange.turning_on
        else:
            self.read_config()
            return StateChange.turning_off


class CronWithHdmi(Cron):
    def check(self) -> StateChange:
        state_change = super().check()
        if state_change == StateChange.turning_on:
            subprocess.run(ARGS_CEC_CLIENT, input=b"on 0\n")
        elif state_change == StateChange.turning_off:
            subprocess.run(ARGS_CEC_CLIENT, input=b"standby 0\n")
        return state_change


def parse_schedule(raw_schedule: t.Dict[str, t.List[str]]) -> t.List[t.List[time]]:
    weekdays: list[str] = "Mon Tue Wed Thu Fri Sat Sun".split()  # type: ignore
    result = [[] for _ in weekdays]
    for weekday, times in raw_schedule.items():
        target = result[weekdays.index(weekday)]
        for item in times:
            match = re.match(r"^(\d+):(\d+)\s*-\s*(\d+):(\d+)$", item)
            assert match is not None, f"Bad time range: {item}"
            hour1, minute1, hour2, minute2 = map(int, match.groups())
            target.append(time(hour1, minute1))
            target.append(time(hour2, minute2))
    return result
