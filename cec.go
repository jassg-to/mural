package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// CEC controls a connected display via the cec-client CLI tool.
// On platforms where cec-client is not available, all operations are no-ops.
type CEC struct {
	available bool
}

func NewCEC() *CEC {
	_, err := exec.LookPath("cec-client")
	if err != nil {
		log.Printf("cec-client not found in PATH; CEC control disabled")
		return &CEC{available: false}
	}
	return &CEC{available: true}
}

// TurnOn sends the HDMI CEC "on" command to the connected display.
func (c *CEC) TurnOn() error {
	return c.send("on 0")
}

// TurnOff sends the HDMI CEC "standby" command to the connected display.
func (c *CEC) TurnOff() error {
	return c.send("standby 0")
}

func (c *CEC) send(command string) error {
	if !c.available {
		return nil
	}
	cmd := exec.Command("cec-client", "-s", "-d", "1")
	cmd.Stdin = strings.NewReader(command + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cec-client %q: %w (output: %s)", command, err, out)
	}
	log.Printf("CEC: %s", command)
	return nil
}
