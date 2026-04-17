//go:build windows

package tun

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/songgao/water"
)

func platformConfigureTUN(cfg *water.Config) {
	cfg.PlatformSpecificParams = water.PlatformSpecificParams{
		ComponentID: "tap0901",
		InterfaceName: "tun0",
	}
}

func runCmd(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return nil
}

func getDefaultGateway() (gateway, iface string, err error) {
	// Parse "route print 0.0.0.0" to find the default gateway
	out, err := exec.Command("route", "print", "0.0.0.0").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("route print: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		// Looking for: "0.0.0.0  0.0.0.0  <gateway>  <iface>  <metric>"
		if len(fields) >= 4 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" {
			return fields[2], fields[3], nil
		}
	}
	return "", "", fmt.Errorf("default gateway not found in route table")
}

func configureInterface(name string) error {
	// On Windows, the TAP adapter needs IP configuration via netsh
	return runCmd("netsh", "interface", "ip", "set", "address",
		fmt.Sprintf("name=%s", name),
		"static", TunAddr, TunMask, TunGateway)
}

func setupRoutes(serverIP, origGW, origIF, tunName string) error {
	// 1. Bypass route for the SOCKS5 server
	if err := runCmd("route", "add", serverIP, "mask", "255.255.255.255", origGW, "metric", "5"); err != nil {
		return fmt.Errorf("server bypass route: %w", err)
	}

	// 2. Two covering routes through the TUN gateway
	if err := runCmd("route", "add", "0.0.0.0", "mask", "128.0.0.0", TunGateway, "metric", "10"); err != nil {
		return fmt.Errorf("route 0.0.0.0/1: %w", err)
	}
	if err := runCmd("route", "add", "128.0.0.0", "mask", "128.0.0.0", TunGateway, "metric", "10"); err != nil {
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

	capture(runCmd("route", "delete", "0.0.0.0", "mask", "128.0.0.0", TunGateway))
	capture(runCmd("route", "delete", "128.0.0.0", "mask", "128.0.0.0", TunGateway))
	capture(runCmd("route", "delete", serverIP, "mask", "255.255.255.255", origGW))

	return firstErr
}
