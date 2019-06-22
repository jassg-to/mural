# Equipment

Perform these steps from a regular Windows, Mac or Linux computer.

Microsoft Windows does not come with an SSH client, so you'll need to install
    [PuTTY](https://www.chiark.greenend.org.uk/~sgtatham/putty).

1. Buy Raspberry Pi kit. I bought a [CanaKit](https://canakit.com) model from Amazon.
    ![Pi, power supply, HDMI cable, case, SD card.](kit.jpg)
1. Download and install [Etcher](https://www.balena.io/etcher) or any other SD card flash tool.
1. Download [Raspbian Lite](https://www.raspberrypi.org/downloads/raspbian), unpack it, and flash the `.img` file.
    ![Screenshot of Etcher.](etcher.png)
1. After flashing is completed, eject this device (do it in software first).
1. Fit the card in the specific SD card slot under the Raspberry Pi board.
1. Assemble the rest of the Raspberry Pi kit, along with a keyboard, and a TV or monitor.
1. Connect the power supply last.

# Operating System

When you first turn the Raspberry Pi device, a bunch of things will fly by, until you get to the login prompt:

```text
Raspbian GNU/Linux 9 raspberrypi tty1
raspberrypi login: _
```

1. Enter the default login `pi` and the default password `raspberry`
1. Run the command `sudo raspi-config`
1. Change User Password. 💣 **YOU ABSOLUTELY MUST DO THIS!** 💣
    Untargeted attacks like ransomware, spambot, DDoS zombies, etc. rely heavily on spread through
    [default passwords](https://www.us-cert.gov/ncas/alerts/TA13-175A) in private and public networks.
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

# Supporting Software

This command will install all the remaining dependencies using [this script](../install.sh):

```text
curl -L https://raw.githubusercontent.com/jassg-to/mural-digital/leaner/raspberry-pi/install.sh | bash
```
