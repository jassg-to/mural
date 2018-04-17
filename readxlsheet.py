#!/usr/bin/env python3
from argparse import ArgumentParser
from datetime import timedelta
from os import path, makedirs
from re import compile, search
from typing import Iterable

from apiclient import discovery
from httplib2 import Http
from oauth2client import client
from oauth2client import tools
from oauth2client.file import Storage

from common import settings, ScheduleRule

flags = ArgumentParser(parents=[tools.argparser]).parse_args()

# If modifying these scopes, delete your previously saved credentials
# at ~/.credentials/sheets.googleapis.com-python-quickstart.json
SCOPES = 'https://www.googleapis.com/auth/spreadsheets.readonly'
CLIENT_SECRET_FILE = 'client_secret.json'
APPLICATION_NAME = 'Google Sheets API Python Quickstart'


def get_credentials():
    """Gets valid user credentials from storage.

    If nothing has been stored, or if the stored credentials are invalid,
    the OAuth2 flow is completed to obtain the new credentials.

    Returns:
        Credentials, the obtained credential.
    """
    home_dir = path.expanduser('~')
    credential_dir = path.join(home_dir, '.credentials')
    if not path.exists(credential_dir):
        makedirs(credential_dir)
    credential_path = path.join(credential_dir, 'sheets.googleapis.com-python-quickstart.json')

    store = Storage(credential_path)
    credentials = store.get()
    if not credentials or credentials.invalid:
        flow = client.flow_from_clientsecrets(CLIENT_SECRET_FILE, SCOPES)
        flow.user_agent = APPLICATION_NAME
        credentials = tools.run_flow(flow, store, flags)
        print('Storing credentials to ' + credential_path)
    return credentials


def get_schedule() -> Iterable[ScheduleRule]:
    # This declares the Google Sheets API
    credentials = get_credentials()
    http = credentials.authorize(Http())
    discovery_url = 'https://sheets.googleapis.com/$discovery/rest?version=v4'
    service = discovery.build('sheets', 'v4', http=http, discoveryServiceUrl=discovery_url)

    # This declares the sheet/range we want and fetches its data
    spreadsheet_id = search(r'/spreadsheets/d/([^/]+)', settings.schedule_sheet).group(1)
    range_name = settings.schedule_range
    result = service.spreadsheets().values().get(spreadsheetId=spreadsheet_id, range=range_name).execute()
    values = result.get('values', [])

    # We'll only use cells that look like a time (e.g.: 3:45)
    regex = compile(r'^(\d+):(\d+)$')

    # Column meanings are hardcoded
    # 'day' has a double meaning:
    #   0  means daily;         also means the poll action
    #  1-7 means monday-sunday; also means the normal on/off action
    # Jagged ordering takes into account how the spreadsheet was designed
    columns = [(day, action) for day in (7, 1, 2, 3, 4, 5, 6, 0) for action in ('on', 'off')]

    # Rules will be returned as found on a horizontal scan
    for row in values:
        for (day, action), value in zip(columns, row):
            match = regex.match(value) if value else None
            if match:
                hour, minute = map(int, match.groups())
                time = timedelta(days=day, hours=hour, minutes=minute)
                yield ScheduleRule(time, action if day else 'poll')


def test():
    for i in sorted(get_schedule()):
        print(i)


if __name__ == '__main__':
    test()
