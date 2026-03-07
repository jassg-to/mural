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
6. Connect keyboard and HDMI cable.
7. Connect the power supply last.

The initial setup will go through several screens and reboot once or twice. This is expected.


## First Boot

1. You will be prompted to create a username and password. Create them and remember them.
2. You will eventually see a prompt like `raspberrypi login:`. Log in with the username and password you created.
3. Type `sudo raspi-config` and press Enter. Navigate the menu:
   - **System Options** ▶ **Wireless LAN** — enter your Wi-Fi network name and passphrase.
   - **Localisation Options** ▶ **Timezone** — select the closest location.
4. Select **Finish**. You do not need to reboot yet.


## Install mural-digital

Run this single command:

```
curl -fsSL https://raw.githubusercontent.com/jassg-to/mural-digital/main/install.sh | bash
```

The installer will:
- Install required packages (`xinit`, `ratpoison`, `cec-utils`)
- Download the `mural-digital` binary
- Set up your window manager config
- Create `~/mural-digital/content/` with a sample schedule

If you are running directly on the console (tty1) and have admin access, the installer will also offer to configure **automatic startup**: the Pi will log in and launch the display automatically on every boot.


## Add Your Images

Copy JPG or PNG images into `~/mural-digital/content/`. You can do this over SSH or with a USB drive.

Optionally edit `~/mural-digital/content/schedule.toml` to set the hours when the display should be on.


## Run

Type `startx` and press Enter. The slideshow will launch.

Press any arrow key to manually advance slides. The display will turn off and on automatically according to your schedule.


## Automatic Startup

If you accepted the automatic startup option during installation, the Pi will launch the display on its own after every reboot. To reboot now:

```
sudo reboot
```

If you skipped that option and want to enable it later, re-run the installer:

```
curl -fsSL https://raw.githubusercontent.com/jassg-to/mural-digital/main/install.sh | bash
```

Make sure you are logged in directly on the console (not over SSH) so the installer can detect the tty and offer the full setup.
