from collections import namedtuple
from datetime import timedelta, datetime, time
from typing import TypeVar, Callable, Iterable, Tuple

import yaml

T = TypeVar('T')


class Settings:
    def __init__(self):
        doc = yaml.load(open('settings.yaml'))
        self.run_board = doc['run board']
        self.schedule_sheet = doc['schedule']['sheet']
        self.schedule_poll_at = doc['schedule']['poll at']


# noinspection: PyTypeChecker
def lazy(builder: Callable[[], T]) -> T:
    class Lazy:
        instance = None

        def __getattr__(self, item):
            if self.instance is None:
                self.instance = builder()
            return getattr(self.instance, item)

    return Lazy()


class ScheduleRule(namedtuple('ScheduleRule', ('when', 'what'))):
    pass


def get_next_schedule(present: datetime, schedule: timedelta) -> datetime:
    if schedule.days:
        # Weekly schedules
        last_monday_midnight = datetime.combine(present.date() - timedelta(present.weekday()), time())
        next_schedule = last_monday_midnight + schedule - timedelta(days=1)
        while next_schedule < present:
            next_schedule += timedelta(days=7)
    else:
        # Daily schedules
        last_midnight = datetime.combine(present.date(), time())
        next_schedule = last_midnight + schedule
        while next_schedule < present:
            next_schedule += timedelta(days=1)

    return next_schedule


class Schedule:
    def __init__(self, getter: Callable[[], Iterable[ScheduleRule]]):
        self.get = getter
        self.rules = []
        self.get_rules()

    def get_rules(self):
        absolute_rules = self.get()
        now = datetime.now()
        self.rules = [ScheduleRule(get_next_schedule(now, r.when), r.what) for r in absolute_rules]
        self.rules.sort()

    def calculate(self) -> Tuple[Iterable[str], int, Iterable[str]]:
        agenda = ('%s: %s' % r for r in self.rules[:3])
        # TODO
        return agenda, 1000, []


settings = lazy(Settings)
