package tunnel

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// ServerConfig holds tunnel server configuration.
type ServerConfig struct {
	BindAddr string
	Port     int
}

// Server accepts binary tunnel connections and relays traffic.
type Server struct {
	cfg      ServerConfig
	listener net.Listener
}

// NewServer creates a tunnel server.
func NewServer(cfg ServerConfig) *Server {
	return &Server{cfg: cfg}
}

// ListenAndServe starts the tunnel server. Blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.BindAddr, s.cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("tunnel listen on %s: %w", addr, err)
	}
	s.listener = ln

	log.Printf("[Tunnel] Listening on %s", addr)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				log.Printf("[Tunnel] Accept error: %v", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	// Read the 8-byte connect header with a deadline
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	hdr := make([]byte, HeaderSize)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		conn.Write([]byte{StatusProtoError})
		return
	}

	req, err := UnmarshalConnectRequest(hdr)
	if err != nil {
		log.Printf("[Tunnel] Bad header from %s: %v", conn.RemoteAddr(), err)
		conn.Write([]byte{StatusProtoError})
		return
	}

	// Clear the read deadline for the relay phase
	conn.SetReadDeadline(time.Time{})

	dst := net.JoinHostPort(req.DstIP.String(), fmt.Sprintf("%d", req.DstPort))

	switch req.Protocol {
	case ProtoTCP:
		s.relayTCP(conn, dst)
	case ProtoUDP:
		s.relayUDP(conn, dst)
	default:
		conn.Write([]byte{StatusProtoError})
	}
}

func (s *Server) relayTCP(client net.Conn, dst string) {
	remote, err := net.DialTimeout("tcp", dst, 10*time.Second)
	if err != nil {
		log.Printf("[Tunnel] TCP dial %s failed: %v", dst, err)
		client.Write([]byte{StatusDialFailed})
		return
	}
	defer remote.Close()

	// Signal success — 1 byte, then pure bidirectional relay
	if _, err := client.Write([]byte{StatusOK}); err != nil {
		return
	}

	relay(client, remote)
}

func (s *Server) relayUDP(client net.Conn, dst string) {
	remote, err := net.DialTimeout("udp", dst, 5*time.Second)
	if err != nil {
		log.Printf("[Tunnel] UDP dial %s failed: %v", dst, err)
		client.Write([]byte{StatusDialFailed})
		return
	}
	defer remote.Close()

	if _, err := client.Write([]byte{StatusOK}); err != nil {
		return
	}

	relay(client, remote)
}

// relay performs zero-copy bidirectional data transfer.
func relay(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	pipe := func(dst, src net.Conn) {
		defer wg.Done()
		buf := make([]byte, 32*1024) // 32KB buffer
		io.CopyBuffer(dst, src, buf)
		// Half-close: signal EOF to the other side
		if tc, ok := dst.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}

	go pipe(a, b)
	go pipe(b, a)
	wg.Wait()
}
