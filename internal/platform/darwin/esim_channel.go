package darwin

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

type usbATESIMChannel struct {
	command func(string, time.Duration) (string, error)
	channel byte
	mu      sync.Mutex
}

func newUSBATESIMChannel(command func(string, time.Duration) (string, error)) *usbATESIMChannel {
	return &usbATESIMChannel{command: command}
}

func (c *usbATESIMChannel) Connect() error       { return nil }
func (c *usbATESIMChannel) Disconnect() error    { return nil }
func (c *usbATESIMChannel) CurrentChannel() byte { return c.channel }

func (c *usbATESIMChannel) OpenLogicalChannel(aid []byte) (byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	response, err := c.command(fmt.Sprintf(`AT+CCHO="%s"`, strings.ToUpper(hex.EncodeToString(aid))), 8*time.Second)
	if err != nil {
		return 0, err
	}
	match := regexp.MustCompile(`\+CCHO:\s*(\d+)`).FindStringSubmatch(response)
	if len(match) != 2 {
		return 0, fmt.Errorf("AT+CCHO did not return a logical channel: %s", response)
	}
	var channel int
	if _, err := fmt.Sscanf(match[1], "%d", &channel); err != nil || channel <= 0 || channel > 255 {
		return 0, fmt.Errorf("invalid logical channel %q", match[1])
	}
	c.channel = byte(channel)
	return c.channel, nil
}

func (c *usbATESIMChannel) Transmit(command []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.channel == 0 {
		return nil, fmt.Errorf("eUICC logical channel is not open")
	}
	commandHex := strings.ToUpper(hex.EncodeToString(command))
	response, err := c.command(fmt.Sprintf(`AT+CGLA=%d,%d,"%s"`, c.channel, len(commandHex), commandHex), 15*time.Second)
	if err != nil {
		return nil, err
	}
	match := regexp.MustCompile(`\+CGLA:\s*\d+\s*,\s*"?([0-9A-Fa-f]+)"?`).FindStringSubmatch(response)
	if len(match) != 2 {
		return nil, fmt.Errorf("AT+CGLA did not return an APDU response: %s", response)
	}
	return hex.DecodeString(match[1])
}

func (c *usbATESIMChannel) CloseLogicalChannel(channel byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.command(fmt.Sprintf("AT+CCHC=%d", channel), 8*time.Second)
	if c.channel == channel {
		c.channel = 0
	}
	return err
}
