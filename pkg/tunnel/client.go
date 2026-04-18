package tunnel

import (
	"fmt"
	"io"
	"net"
	"time"
)

// Client dials the tunnel server with the binary protocol.
type Client struct {
	serverAddr string
}

// NewClient creates a tunnel client targeting the given server address.
func NewClient(serverAddr string) *Client {
	return &Client{serverAddr: serverAddr}
}

// DialTCP connects to a remote TCP destination through the tunnel server.
// Returns a net.Conn that is a raw bidirectional stream to the destination.
func (c *Client) DialTCP(dstIP net.IP, dstPort uint16) (net.Conn, error) {
	return c.dial(ProtoTCP, dstIP, dstPort)
}

// DialUDP connects to a remote UDP destination through the tunnel server.
func (c *Client) DialUDP(dstIP net.IP, dstPort uint16) (net.Conn, error) {
	return c.dial(ProtoUDP, dstIP, dstPort)
}

func (c *Client) dial(proto byte, dstIP net.IP, dstPort uint16) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", c.serverAddr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to tunnel server %s: %w", c.serverAddr, err)
	}

	// Send the 8-byte header
	req := &ConnectRequest{
		Version:  ProtoVersion,
		Protocol: proto,
		DstIP:    dstIP.To4(),
		DstPort:  dstPort,
	}
	if _, err := conn.Write(req.Marshal()); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send connect header: %w", err)
	}

	// Read 1-byte status
	status := make([]byte, ResponseSize)
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if _, err := io.ReadFull(conn, status); err != nil {
		conn.Close()
		return nil, fmt.Errorf("read connect response: %w", err)
	}
	conn.SetReadDeadline(time.Time{})

	if status[0] != StatusOK {
		conn.Close()
		return nil, fmt.Errorf("tunnel connect failed: status 0x%02x", status[0])
	}

	return conn, nil
}
