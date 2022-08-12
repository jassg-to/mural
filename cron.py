#!/usr/bin/env python3
import os.path
import re
import subprocess
import tempfile

import yaml

here = os.path.split(os.path.realpath(__file__))[0]


def main():
    git_pull()
    schedule = get_schedule()
    table = ''.join(emit(parse(schedule)))
    print("The new cron table is:")
    print(table)
    set_crontab(table)


def git_pull():
    os.chdir(here)
    subprocess.call(['/usr/bin/git', 'pull'])


def get_schedule():
    name = os.path.join(here, 'schedule.yaml')
    file = open(name, 'r')
    data = yaml.safe_load(file)
    return data


def parse(schedule):
    for weekday, items in schedule.items():
        for item in items:

            # Try on-off range:
            match = re.match(r"^(\d+):(\d+)\s*-\s*(\d+):(\d+)$", item)
            if match:
                yield (weekday, match.group(1), match.group(2), 'on.sh')
                yield (weekday, match.group(3), match.group(4), 'off.sh')
                continue

            # Try single event command
            match = re.match(r"(\D.+)\s+(\d+):(\d+)", item)
            if match:
                yield (weekday, match.group(2), match.group(3), match.group(1))
                continue

            # Something went wrong!
            raise ValueError("Could not understand: " + item)


def emit(parsed_jobs):
    for weekday, hour, minute, job in parsed_jobs:
        full_path = os.path.join(here, job)
        yield "%2s %2s * * %3s\t%s\n" % (minute, hour, weekday, full_path)


def set_crontab(table):
    file = tempfile.NamedTemporaryFile('w', delete=False)
    file.write(table)
    file.close()
    subprocess.call(["/usr/bin/crontab", file.name])
    os.unlink(file.name)


if __name__ == '__main__':
    main()
