## Equipment

Perform these steps from a separate Windows, Mac or Linux computer.

1. Buy Raspberry Pi kit. I bought a [CanaKit](https://canakit.com) model from Amazon.
    ![Pi, power supply, HDMI cable, case, SD card.](kit.jpg)
2. Download and install [Raspberry Pi Imager](https://www.raspberrypi.com/software/).
3. Make sure the newly purchased SD card is connected to your computer. In Raspberry Pi Imager, select:
   1. **Choose OS**
   2. Other specific-purpose OS
   3. [FullPageOS](https://github.com/guysoft/FullPageOS)
   4. FullPageOS (Stable)
   5. **Choose Storage**
   6. Select your SD card. If more than one option shows up, check carefully for the correct choice and only proceed if 100% sure.
   7. **Write**
4. From this point on, Windows will sometimes ask if you want to format the device. **Always say no.**


## Pre-boot Configuration

1. Eject the SD Card and reconnect it back to the same computer.
2. You will see a new disk drive with a lot of strange files. We will edit some of these.
3. To set up Wi-Fi, edit `fullpageos-wpa-supplicant.txt`. Follow the instructions inside that file.
4. Edit `fullpageos.txt` and put your URL there. Here's an example:

       https://docs.google.com/presentation/d/e/2PACX-1vTk-Y8BmlJJavFK2ZpKTZ_2dvMMqC7-19C3g54cP0tYP6yjzMzHdGqMFOIiMrrg6DjpteXT647axciL/pub?start=true&loop=true&delayms=30000

5. How do you plan to update the schedule?
   * To just have it sitting in the SD Card, copy the example `schedule.yaml` file provided into that folder. Make sure it has the correct values that make sense to you.
   * To pull it from an external source, adapt the given yaml sample to your needs (make sure you have a `cron` entry so that the schedule self-updates) and write the URL into a new file called `mural-digital.txt`.
6. Eject the SD Card, insert into the Raspberry Pi board, and assemble and plug all other components.
7. Plug the power source last.


## Post-boot Configuration

At this point you should have visible digital signage.
Read the [FullPageOS](https://github.com/guysoft/FullPageOS) documentation about how to change all the default passwords.
Spambots are a real threat everywhere.


## Install the HDMI power schedule

In order to save power and prevent volunteers from pulling the plug, we turn the screen off and on using a schedule.
You can see a sample schedule file in the file [`schedule.yaml`](../content/schedule.yaml).

1. [Connect to your Raspberry Pi via SSH.](https://www.google.com/search?q=how+to+ssh+into+raspberry+pi)
2. Run the command `sudo raspi-config`
    * Use the keyboard arrows to navigate. 
    * Select `Localisation Options` ▶ `Change Timezone`.
    * Select the closest relevant continent then city, e.g. `America`, then `Toronto`.
    * Select `System` ▶ `Password`. This will change the default SSH password to anything else.
    * Select `Display` ▶ `Underscan` ▶ select `No`.
    * Select `Finish`. It will offer to reboot, say `Yes`.
3. Install the tools:
    * `sudo apt update`
    * `sudo apt install -y cec-utils xdotool python3-pip`
4. Clone this repository:
    * `git clone https://github.com/jassg-to/mural-digital.git`
5. Install the schedule:
    * `mural-digital/cron.py`

And done! You can press `Ctrl`+`D` to leave and `Alt`+`F2` to return to the signage.
