package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"lannel/pkg/proxy"
	"lannel/pkg/web"
)

func main() {
	bindAddr := flag.String("bind", "0.0.0.0", "Address to bind services on")
	socksPort := flag.Int("socks-port", 1080, "SOCKS5 proxy listen port")
	httpPort := flag.Int("http-port", 8080, "Web UI listen port")
	allowedSubnet := flag.String("allowed-subnet", "", "Restrict SOCKS5 to a CIDR (e.g., 192.168.1.0/24). Empty = allow all")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 2)

	// --- Service A: SOCKS5 Proxy ---
	proxyServer := proxy.New(proxy.Config{
		BindAddr:      *bindAddr,
		Port:          *socksPort,
		AllowedSubnet: *allowedSubnet,
	})
	go func() {
		errCh <- proxyServer.ListenAndServe(ctx)
	}()

	// --- Service B: Web UI ---
	webServer, err := web.New(web.Config{
		BindAddr:  *bindAddr,
		HTTPPort:  *httpPort,
		SocksPort: *socksPort,
	})
	if err != nil {
		log.Fatalf("[LANnel Server] Web UI init failed: %v", err)
	}
	go func() {
		errCh <- webServer.ListenAndServe(ctx)
	}()

	log.Printf("[LANnel Server] Started (SOCKS5 :%d | Web UI :%d)", *socksPort, *httpPort)

	select {
	case sig := <-sigCh:
		fmt.Printf("\n[LANnel Server] Received %v, shutting down...\n", sig)
		cancel()
	case err := <-errCh:
		if err != nil {
			log.Fatalf("[LANnel Server] Fatal: %v", err)
		}
	}

	log.Println("[LANnel Server] Stopped.")
}
