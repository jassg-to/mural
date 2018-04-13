#!/usr/bin/env python3
from argparse import ArgumentParser
from os import path, makedirs
from re import compile

from httplib2 import Http
from apiclient import discovery
from oauth2client import client
from oauth2client import tools
from oauth2client.file import Storage

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


def main():
    # TODO: Check back every day for rule changes

    # This declares the Google Sheets API
    credentials = get_credentials()
    http = credentials.authorize(Http())
    discovery_url = 'https://sheets.googleapis.com/$discovery/rest?version=v4'
    service = discovery.build('sheets', 'v4', http=http, discoveryServiceUrl=discovery_url)

    # This declares the sheet/range we want and fetches its data
    spreadsheet_id = 'haha good one'
    range_name = 'Sheet1!A3:N'
    result = service.spreadsheets().values().get(spreadsheetId=spreadsheet_id, range=range_name).execute()
    values = result.get('values', [])

    # We'll only use cells that look like a time (3:45)
    regex = compile(r'^(\d+):(\d+)$')
    # Column meanings are hardcoded
    day_names = 'Sun Mon Tue Wed Thu Fri Sat'.split()
    columns = [(a, b) for a in range(7) for b in ('on', 'off')]
    # Rules will be stored as a flat list
    rules = []
    for row in values:
        for (day, action), value in zip(columns, row):
            match = regex.match(value) if value else None
            if match:
                hour, minute = map(int, match.groups())
                rules.append((day, hour, minute, action))
    rules.sort()
    return rules


if __name__ == '__main__':
    main()
