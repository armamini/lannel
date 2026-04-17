//go:build linux

package tun

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/songgao/water"
)

func platformConfigureTUN(cfg *water.Config) {
	cfg.Name = "tun0"
}

func runCmd(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return nil
}

func getDefaultGateway() (gateway, iface string, err error) {
	out, err := exec.Command("ip", "route", "show", "default").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("ip route show default: %w", err)
	}
	// Expected format: "default via 192.168.1.1 dev eth0 ..."
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 5 || fields[0] != "default" {
		return "", "", fmt.Errorf("unexpected route output: %s", string(out))
	}
	return fields[2], fields[4], nil
}

func configureInterface(name string) error {
	if err := runCmd("ip", "addr", "add", TunCIDR, "dev", name); err != nil {
		return err
	}
	if err := runCmd("ip", "link", "set", "dev", name, "mtu", fmt.Sprintf("%d", DefaultMTU)); err != nil {
		return err
	}
	return runCmd("ip", "link", "set", "dev", name, "up")
}

func setupRoutes(serverIP, origGW, origIF, tunName string) error {
	// 1. Static route to the SOCKS5 server via the original gateway
	//    so proxy control traffic never enters the TUN loop.
	if err := runCmd("ip", "route", "add", serverIP+"/32", "via", origGW, "dev", origIF); err != nil {
		return fmt.Errorf("server bypass route: %w", err)
	}

	// 2. Replace default route with two covering routes through TUN.
	//    0.0.0.0/1 and 128.0.0.0/1 together cover all IPs but are more
	//    specific than 0.0.0.0/0, so the original default stays as fallback.
	if err := runCmd("ip", "route", "add", "0.0.0.0/1", "dev", tunName); err != nil {
		return fmt.Errorf("route 0.0.0.0/1: %w", err)
	}
	if err := runCmd("ip", "route", "add", "128.0.0.0/1", "dev", tunName); err != nil {
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

	capture(runCmd("ip", "route", "del", "0.0.0.0/1"))
	capture(runCmd("ip", "route", "del", "128.0.0.0/1"))
	capture(runCmd("ip", "route", "del", serverIP+"/32", "via", origGW, "dev", origIF))

	return firstErr
}
