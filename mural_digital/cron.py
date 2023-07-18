import enum
import re
import subprocess
import sys
import typing as t
from bisect import bisect_right
from datetime import datetime, time

import yaml
from mural_digital import CONTENT_PATH

ARGS_CEC_CLIENT = ("/usr/bin/cec-client", "-s")
CONFIG_YAML_PATH = CONTENT_PATH / "config.yaml"


class StateChange(enum.IntEnum):
    no_change = 0
    turning_on = 1
    turning_off = 2


class Options(t.NamedTuple):
    slide_time_seconds: int


class CronShim:
    def __init__(self):
        self.options, self.schedule = read_config()

    def check(self):
        return StateChange.no_change


class Cron(CronShim):
    def __init__(self):
        super().__init__()
        self.state = True
        self._current_weekday = datetime.now().weekday()
        self._today_list = self.schedule[self._current_weekday]

    def check(self) -> StateChange:
        now = datetime.now()
        weekday = now.weekday()
        if weekday != self._current_weekday:
            sys.exit(0)  # restart to pick up updates

        current_spot = bisect_right(self._today_list, now.time())
        new_state = bool(current_spot % 2)
        if new_state == self.state:
            return StateChange.no_change
        self.state = new_state
        if new_state:
            subprocess.run(ARGS_CEC_CLIENT, input=b"on 0\n")
            return StateChange.turning_on
        else:
            subprocess.run(ARGS_CEC_CLIENT, input=b"standby 0\n")
            self.options, self.schedule = read_config()
            return StateChange.turning_off


def read_config() -> t.Tuple[Options, t.List[t.List[time]]]:
    with open(CONFIG_YAML_PATH) as f:
        raw_config = yaml.safe_load(f)
    options = Options(**raw_config["options"])
    schedule = parse_schedule(raw_config["schedule"])
    return options, schedule


def parse_schedule(raw_schedule: t.Dict[str, t.List[str]]) -> t.List[t.List[time]]:
    weekdays = "Mon Tue Wed Thu Fri Sat Sun".split()
    result = [[] for _ in weekdays]
    for weekday, times in raw_schedule.items():
        target = result[weekdays.index(weekday)]
        for item in times:
            match = re.match(r"^(\d+):(\d+)\s*-\s*(\d+):(\d+)$", item)
            hour1, minute1, hour2, minute2 = map(int, match.groups())
            target.append(time(hour1, minute1))
            target.append(time(hour2, minute2))
    return result
