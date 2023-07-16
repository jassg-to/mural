import enum
import re
import subprocess
import typing as t
from bisect import bisect_right
from datetime import datetime, time

import yaml
from mural_digital import CONTENT_PATH, GIT_ROOT

ARGS_CEC_CLIENT = ("/usr/bin/cec-client", "-s")
SCHEDULE_PATH = CONTENT_PATH / "schedule.yaml"


class StateChange(enum.IntEnum):
    no_change = 0
    turning_on = 1
    turning_off = 2


class Cron:
    def __init__(self):
        self.schedule = read_schedule()
        self.state = True

    def check(self) -> StateChange:
        now = datetime.now()
        today_list = self.schedule[now.weekday()]
        current_spot = bisect_right(today_list, now.time())
        new_state = bool(current_spot % 2)
        if new_state == self.state:
            return StateChange.no_change
        self.state = new_state
        if new_state:
            subprocess.run(ARGS_CEC_CLIENT, input=b"on 0\n")
            return StateChange.turning_on
        else:
            subprocess.run(ARGS_CEC_CLIENT, input=b"standby 0\n")
            update_from_git()
            return StateChange.turning_off


def read_schedule() -> t.List[t.List[time]]:
    weekdays = "Mon Tue Wed Thu Fri Sat Sun".split()
    result = [[] for _ in weekdays]
    for weekday, times in yaml.safe_load(SCHEDULE_PATH.open()).items():
        target = result[weekdays.index(weekday)]
        for item in times:
            match = re.match(r"^(\d+):(\d+)\s*-\s*(\d+):(\d+)$", item)
            hour1, minute1, hour2, minute2 = map(int, match.groups())
            target.append(time(hour1, minute1))
            target.append(time(hour2, minute2))
    return result


def update_from_git():
    subprocess.run(["/usr/bin/git", "reset", "--hard", "HEAD"], cwd=GIT_ROOT)
    subprocess.run(["/usr/bin/git", "pull", "--ff-only"], cwd=GIT_ROOT)
