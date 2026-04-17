package tun

import (
	"fmt"
	"log"
	"net"

	"github.com/songgao/water"
)

const (
	// TUN device MTU — matches common VPN MTU to avoid fragmentation.
	DefaultMTU = 1500
	// TUN subnet used for the virtual interface.
	TunAddr    = "10.0.85.1"
	TunGateway = "10.0.85.0"
	TunMask    = "255.255.255.0"
	TunCIDR    = "10.0.85.1/24"
)

// Device wraps a TUN interface with its configuration.
type Device struct {
	Iface      *water.Interface
	Name       string
	ServerIP   string
	OriginalGW string
	OriginalIF string
}

// NewDevice creates and configures a TUN interface.
// serverIP is the SOCKS5 server's LAN IP — we add a static route for it
// so the proxy control traffic bypasses the TUN.
func NewDevice(serverIP string) (*Device, error) {
	if ip := net.ParseIP(serverIP); ip == nil {
		return nil, fmt.Errorf("invalid server IP: %s", serverIP)
	}

	cfg := water.Config{
		DeviceType: water.TUN,
	}
	platformConfigureTUN(&cfg)

	iface, err := water.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("create TUN interface: %w", err)
	}

	dev := &Device{
		Iface:    iface,
		Name:     iface.Name(),
		ServerIP: serverIP,
	}

	log.Printf("[TUN] Created interface: %s", dev.Name)
	return dev, nil
}

// Setup configures the TUN interface IP, brings it up, and reroutes
// the default gateway through the tunnel. It preserves the original
// gateway so it can be restored on teardown.
func (d *Device) Setup() error {
	origGW, origIF, err := getDefaultGateway()
	if err != nil {
		return fmt.Errorf("detect original gateway: %w", err)
	}
	d.OriginalGW = origGW
	d.OriginalIF = origIF
	log.Printf("[TUN] Original gateway: %s via %s", d.OriginalGW, d.OriginalIF)

	if err := configureInterface(d.Name); err != nil {
		return fmt.Errorf("configure interface %s: %w", d.Name, err)
	}
	log.Printf("[TUN] Interface %s configured with %s", d.Name, TunCIDR)

	if err := setupRoutes(d.ServerIP, d.OriginalGW, d.OriginalIF, d.Name); err != nil {
		return fmt.Errorf("setup routes: %w", err)
	}
	log.Printf("[TUN] Routes configured — all traffic routed through %s", d.Name)

	return nil
}

// Teardown restores the original routing table and closes the TUN device.
func (d *Device) Teardown() {
	log.Println("[TUN] Restoring original routes...")

	if err := teardownRoutes(d.ServerIP, d.OriginalGW, d.OriginalIF); err != nil {
		log.Printf("[TUN] Warning: route teardown: %v", err)
	}

	if err := d.Iface.Close(); err != nil {
		log.Printf("[TUN] Warning: close interface: %v", err)
	}

	log.Println("[TUN] Teardown complete.")
}

// Read reads a single IP packet from the TUN device.
func (d *Device) Read(buf []byte) (int, error) {
	return d.Iface.Read(buf)
}

// Write writes a single IP packet to the TUN device.
func (d *Device) Write(buf []byte) (int, error) {
	return d.Iface.Write(buf)
}
