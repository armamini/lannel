package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/armamini/lannel/pkg/tun"
	"github.com/armamini/lannel/pkg/tunnel"
)

func main() {
	serverAddr := flag.String("server", "", "Server LAN IP address (e.g., 192.168.1.10)")
	tunnelPort := flag.Int("port", 9090, "Server tunnel port")
	flag.Parse()

	if *serverAddr == "" {
		fmt.Fprintln(os.Stderr, "Usage: lannel-client -server <SERVER_LAN_IP> [-port 9090]")
		os.Exit(1)
	}

	if ip := net.ParseIP(*serverAddr); ip == nil {
		fmt.Fprintf(os.Stderr, "Invalid server IP: %s\n", *serverAddr)
		os.Exit(1)
	}

	tunnelAddr := fmt.Sprintf("%s:%d", *serverAddr, *tunnelPort)
	log.Printf("[LANnel Client] Target tunnel server: %s", tunnelAddr)

	// --- Create TUN interface ---
	dev, err := tun.NewDevice(*serverAddr)
	if err != nil {
		log.Fatalf("[LANnel Client] TUN creation failed: %v", err)
	}

	// --- Configure routes ---
	if err := dev.Setup(); err != nil {
		dev.Teardown()
		log.Fatalf("[LANnel Client] Route setup failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// --- Graceful shutdown: restore routes on signal ---
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Printf("\n[LANnel Client] Received %v, restoring routes...\n", sig)
		cancel()
	}()

	// --- Start packet forwarding engine ---
	tunnelClient := tunnel.NewClient(tunnelAddr)
	engine := tun.NewEngine(dev, tunnelClient)
	log.Println("[LANnel Client] System-wide tunnel active. Press Ctrl+C to stop.")

	if err := engine.Run(ctx); err != nil {
		log.Printf("[LANnel Client] Engine error: %v", err)
	}

	dev.Teardown()
	log.Println("[LANnel Client] Stopped.")
}
