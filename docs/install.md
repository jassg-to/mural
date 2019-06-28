# Equipment

Perform these steps from a regular Windows, Mac or Linux computer.

1. Buy Raspberry Pi kit. I bought a [CanaKit](https://canakit.com) model from Amazon.
    ![Pi, power supply, HDMI cable, case, SD card.](kit.jpg)
1. Download and install [Etcher](https://www.balena.io/etcher) or any other SD card flash tool.
1. Download [Raspbian Lite](https://www.raspberrypi.org/downloads/raspbian) and flash it to the SD card.
    ![Screenshot of Etcher.](etcher.png)
1. After flashing is completed, eject this device (do it in software first).
1. Fit the card in the specific SD card slot under the Raspberry Pi board.
1. Assemble the rest of the Raspberry Pi kit, along with a keyboard, and a TV or monitor.
1. Connect the power supply last.

# Operating System

When you first turn the Raspberry Pi device, a bunch of things will fly by, until you get to the login prompt:

```text
Raspbian GNU/Linux 10 raspberrypi tty1
raspberrypi login: _
```

1. Enter the default login `pi` and the default password `raspberry`
1. Run the command `sudo raspi-config`
1. Network Options ▶ WiFi
    - Set country correctly.
    - Your network name is the SSID. Be exact, it's like a password.
    - Your network password is the passphrase. Same rules.
1. Boot options ▶ Wait for Network at Boot ▶ Yes
1. Localisation Options ▶ Change Timezone
1. Finish.
1. To confirm that everything was set up correctly, run these commands to check:
    - `date` to see current time.
        - If timezone is incorrect, run `sudo raspi-config` again and fix it.
        - If timezone is correct but time is wrong, see [`date`'s manual](https://linux.die.net/man/1/date) to fix it.
    - `ip a` to see current IP address.
        - Check `inet` under `wlan0` for WiFi.
        - If there are no IP addresses under `wlan0`, run `sudo raspi-config` again and re-configure WiFi.

# Install Schedule Tools

Run these commands to install some dependencies:

* `sudo apt update`
* `sudo apt install git cec-client`

Now run the `git` command to clone this repository into a folder called `md`. Example:

* `git clone https://github.com/jassg-to/mural-digital.git /home/pi/md`

Finally, install the schedule:

* `sudo ln -s /home/pi/md/crontab /etc/crontab.d/md`

You can edit that file to update the schedule. Please read the comments carefully.

# Install and configure display tools

Finally install whatever you're using for digital signage.

We're using [Screenly](https://screenly.io). This will take 15 minutes to a few hours.

```text
bash <(curl -sL https://www.screenly.io/install-ose.sh)
```

It'll ask you a few questions at the beginning.
If you're unsure what to answer, press Enter to choose the suggested answer.
After these questions, you can disconnect the keyboard because you no longer need it.

At the end of the installation it'll show you a web address for you to visit from a normal computer.
That's where you set the web address.

To get your web address for Google Presentations, you gotta open your presentation,
go to the *File* menu and select *Publish to the web...*.

![Publish to the web. Auto-advance every 30 seconds. Auto-start at load. Auto-restart after last.](publish.png)

And you'll get an URL like this to add as an asset (and activate) to Screenly:

```text
https://docs.google.com/presentation/d/e/2PACX-1vQ7LGi9WeOpcex-d2VXgQeT4pfHqd9h3YXWkDr9iReuKIIQMzPNBVZ5-J5xEh6wqvyO_aK858H4nQto/pub?start=true&loop=true&delayms=30000
```
