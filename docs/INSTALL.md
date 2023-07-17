## Equipment

Perform these steps from a separate Windows, Mac or Linux computer.

1. Buy Raspberry Pi kit. I bought a [CanaKit](https://canakit.com) model from Amazon.
    ![Pi, power supply, HDMI cable, case, SD card.](kit.jpg)
2. Download and install [Raspberry Pi Imager](https://www.raspberrypi.com/software/).
3. Make sure the newly purchased SD card is connected to your computer. In Raspberry Pi Imager, select:
   1. **Choose OS**
   2. Raspberry Pi OS (other)
   3. Raspberry Pi OS Lite (64-bit)
   4. **Choose Storage**
   5. Select your SD card. Check carefully for the correct choice and only proceed if 100% sure.
   6. **Write**
4. From this point on, Windows will sometimes ask if you want to format the device. **Always say no.**
5. Insert the SD card into the Raspberry Pi board.
6. Connect keyboard, mouse, and HDMI cable.
7. Connect the power supply last.

The initial setup will go through several screens and reboot once or twice. This is expected.


## Configuration

1. You should see a screen with a blue background and a terminal window. Just answer the questions.
2. You will eventually see a prompt like this:

       pi@raspberrypi:~ $

3. Type `sudo raspi-config` and press `Enter`. This will open a blue screen with a menu.
4. Open `System Options` ▶ `Boot / Auto Login` ▶ `Console Autologin`.
5. Open `Network Options` ▶ `Wi-Fi`. Select your Wi-Fi network and enter the password.
6. Open `Localisation Options` ▶ `Change Timezone`. Select the closest relevant location.
7. This is a good opportunity to explore other administrative options, like SSH.
8. Once you're done, select `Finish` and don't reboot yet.
9. Install git with `sudo apt install git`
10. Clone this repository with `git clone https://github.com/jassg-to/mural-digital.git`
11. Run the installer with `mural-digital/install.sh`
12. Reboot with `sudo reboot`
