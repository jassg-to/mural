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
1. Assemble the rest of the Raspberry Pi kit and turn it on.
    Remember to connect the power supply last.

# Operating System

The first and most important task is changing the password.
**YOU MUST DO THIS!** Untargeted attacks like ransomware, spambot, DDoS zombies, etc. rely heavily on spread through
[default passwords](https://www.us-cert.gov/ncas/alerts/TA13-175A) in private and public networks.
