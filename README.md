# Mural Digital

Simple digital signage using a web browser and any publicly acessible web page.

Works great with [Google Slides](https://www.google.com/slides/about).

## Setup

### Hardware

1. Buy Raspberry Pi kit. I bought a [CanaKit](https://canakit.com) model from Amazon.
1. On a normal computer, download and install [Etcher](https://www.balena.io/etcher) or any other SD card flash tool.
1. Download [Raspbian Lite](https://www.raspberrypi.org/downloads/raspbian), unpack it, and flash the `.img` file.
    ![Scheenshot of etcher.](docs/etcher.png)
1. After flashing is completed, eject and re-insert the SD card into your computer.
    The files created in next steps must be inside the card, still from a normal computer.
1. Create a `wpa_supplicant.conf` file with your WiFi configuration
    ([more info](https://www.raspberrypi.org/documentation/configuration/wireless/wireless-cli.md)).

### Operating System
