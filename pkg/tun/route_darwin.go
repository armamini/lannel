//go:build darwin

package tun

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/songgao/water"
)

func platformConfigureTUN(cfg *water.Config) {
	// macOS assigns utunN automatically; water handles this.
}

func runCmd(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return nil
}

func getDefaultGateway() (gateway, iface string, err error) {
	out, err := exec.Command("route", "-n", "get", "default").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("route get default: %w", err)
	}

	var gw, ifName string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "gateway:") {
			gw = strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))
		}
		if strings.HasPrefix(line, "interface:") {
			ifName = strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		}
	}

	if gw == "" || ifName == "" {
		return "", "", fmt.Errorf("could not parse default gateway from: %s", string(out))
	}
	return gw, ifName, nil
}

func configureInterface(name string) error {
	// macOS uses ifconfig for TUN: set point-to-point addresses
	if err := runCmd("ifconfig", name, TunAddr, TunGateway, "netmask", TunMask, "up"); err != nil {
		return err
	}
	return runCmd("ifconfig", name, "mtu", fmt.Sprintf("%d", DefaultMTU))
}

func setupRoutes(serverIP, origGW, origIF, tunName string) error {
	// 1. Bypass route for the SOCKS5 server via original gateway
	if err := runCmd("route", "add", "-host", serverIP, origGW); err != nil {
		return fmt.Errorf("server bypass route: %w", err)
	}

	// 2. Two covering routes through the TUN interface
	if err := runCmd("route", "add", "-net", "0.0.0.0/1", "-interface", tunName); err != nil {
		return fmt.Errorf("route 0.0.0.0/1: %w", err)
	}
	if err := runCmd("route", "add", "-net", "128.0.0.0/1", "-interface", tunName); err != nil {
		return fmt.Errorf("route 128.0.0.0/1: %w", err)
	}

	return nil
}

func teardownRoutes(serverIP, origGW, origIF string) error {
	var firstErr error
	capture := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	capture(runCmd("route", "delete", "-net", "0.0.0.0/1"))
	capture(runCmd("route", "delete", "-net", "128.0.0.0/1"))
	capture(runCmd("route", "delete", "-host", serverIP))

	return firstErr
}
