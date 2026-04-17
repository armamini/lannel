package proxy

import (
	"context"
	"fmt"
	"log"
	"net"

	socks5 "github.com/armon/go-socks5"
)

// Config holds SOCKS5 proxy configuration.
type Config struct {
	// BindAddr is the LAN address to listen on (e.g., "0.0.0.0").
	BindAddr string
	// Port is the SOCKS5 listen port.
	Port int
	// AllowedSubnet restricts connections to a specific CIDR (optional).
	// Empty string means allow all.
	AllowedSubnet string
}

// Server wraps the SOCKS5 server with lifecycle management.
type Server struct {
	cfg      Config
	listener net.Listener
}

// New creates a new proxy Server.
func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// ListenAndServe starts the SOCKS5 proxy. It blocks until ctx is cancelled.
// The proxy does NOT bind outbound connections to any specific interface,
// so traffic naturally follows the host OS default route — including any
// active VPN tunnel.
func (s *Server) ListenAndServe(ctx context.Context) error {
	conf := &socks5.Config{
		Logger: log.New(log.Writer(), "[SOCKS5] ", log.LstdFlags),
	}

	// Optional: restrict to a LAN subnet
	if s.cfg.AllowedSubnet != "" {
		_, cidr, err := net.ParseCIDR(s.cfg.AllowedSubnet)
		if err != nil {
			return fmt.Errorf("invalid allowed subnet %q: %w", s.cfg.AllowedSubnet, err)
		}
		conf.Rules = &subnetRule{allowed: cidr}
	}

	srv, err := socks5.New(conf)
	if err != nil {
		return fmt.Errorf("socks5 init: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", s.cfg.BindAddr, s.cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("socks5 listen on %s: %w", addr, err)
	}
	s.listener = ln

	log.Printf("[SOCKS5] Listening on %s", addr)

	// Shutdown listener when context is cancelled
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	if err := srv.Serve(ln); err != nil {
		// Suppress error caused by listener close on shutdown
		select {
		case <-ctx.Done():
			return nil
		default:
			return fmt.Errorf("socks5 serve: %w", err)
		}
	}
	return nil
}

// subnetRule only allows connections originating from a specific CIDR.
type subnetRule struct {
	allowed *net.IPNet
}

func (r *subnetRule) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	addr, _, err := net.SplitHostPort(req.RemoteAddr.String())
	if err != nil {
		log.Printf("[SOCKS5] Denied: cannot parse remote addr %s", req.RemoteAddr)
		return ctx, false
	}
	ip := net.ParseIP(addr)
	if ip == nil || !r.allowed.Contains(ip) {
		log.Printf("[SOCKS5] Denied connection from %s (outside %s)", addr, r.allowed)
		return ctx, false
	}
	return ctx, true
}
