## Equipment

Perform these steps from a separate Windows, Mac or Linux computer.

1. Buy Raspberry Pi kit. I bought a [CanaKit](https://canakit.com) model from Amazon.
    ![Pi, power supply, HDMI cable, case, SD card.](kit.jpg)
2. Download and install [Raspberry Pi Imager](https://www.raspberrypi.com/software/).
3. Make sure the newly purchased SD card is connected to your computer. In Raspberry Pi Imager, select:
   1. **Choose OS**
   2. Raspberry Pi OS (other) ▶ **Raspberry Pi OS Lite** (64-bit)
   4. **Choose Storage**
   5. Select your SD card. Check carefully for the correct choice and only proceed if 100% sure that the disk size matches your SD card.
   6. **Write**
4. From this point on, Windows will sometimes ask if you want to format the device. **Always say no.**
5. Insert the SD card into the Raspberry Pi board.
6. Connect keyboard, mouse, and HDMI cable.
7. Connect the power supply last.

The initial setup will go through several screens and reboot once or twice. This is expected.


## Configuration

1. You should see a screen with a blue background and a terminal window. It will prompt you to create a username and password. Create them and remember them.
2. You will eventually see a prompt like this: `raspberrypi login:`. Log in with the username and password you created.
3. Type `sudo raspi-config` and press `Enter`. This will open a blue screen with a menu. The options below may change over time but they all will exist in some form.
5. Open `System Options` ▶ `Wireless LAN`. Enter your Wi-Fi network name (SSID) and passphrase.
4. Open `System Options` ▶ `Boot` ▶ `Console Text console`.
4. Open `System Options` ▶ `Auto Login` ▶ Yes.
6. Open `Localisation Options` ▶ `Change Timezone`. Select the closest relevant location.
7. This is a good opportunity to explore other administrative options, like SSH.
8. Once you're done, select `Finish` and don't reboot yet.
9. Install git with `sudo apt install git`
10. Clone this repository with `git clone https://github.com/jassg-to/mural-digital.git`
11. Run the installer with `mural-digital/dotfiles/install.sh`
12. Reboot with `sudo reboot`
