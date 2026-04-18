package tunnel

import (
	"encoding/binary"
	"fmt"
	"net"
)

// Wire protocol — minimal binary framing for maximum throughput.
//
// Connect Request (client → server): 8 bytes total
//   [0]     version (0x01)
//   [1]     protocol (0x01 = TCP, 0x02 = UDP)
//   [2:6]   destination IPv4 (4 bytes, big-endian)
//   [6:8]   destination port (2 bytes, big-endian)
//
// Connect Response (server → client): 1 byte
//   [0]     status (0x00 = success, 0x01 = dial failed, 0x02 = protocol error)
//
// After a successful handshake, the connection becomes a raw bidirectional
// byte stream — zero framing overhead on data transfer.

const (
	ProtoVersion = 0x01

	ProtoTCP = 0x01
	ProtoUDP = 0x02

	StatusOK            = 0x00
	StatusDialFailed    = 0x01
	StatusProtoError    = 0x02
	StatusInternalError = 0x03

	HeaderSize   = 8
	ResponseSize = 1
)

// ConnectRequest is the 8-byte binary connect header.
type ConnectRequest struct {
	Version  byte
	Protocol byte
	DstIP    net.IP // IPv4 only (4 bytes)
	DstPort  uint16
}

// Marshal encodes the request into exactly 8 bytes.
func (r *ConnectRequest) Marshal() []byte {
	buf := make([]byte, HeaderSize)
	buf[0] = r.Version
	buf[1] = r.Protocol
	copy(buf[2:6], r.DstIP.To4())
	binary.BigEndian.PutUint16(buf[6:8], r.DstPort)
	return buf
}

// UnmarshalConnectRequest decodes 8 bytes into a ConnectRequest.
func UnmarshalConnectRequest(buf []byte) (*ConnectRequest, error) {
	if len(buf) < HeaderSize {
		return nil, fmt.Errorf("header too short: %d bytes", len(buf))
	}
	if buf[0] != ProtoVersion {
		return nil, fmt.Errorf("unsupported version: 0x%02x", buf[0])
	}
	if buf[1] != ProtoTCP && buf[1] != ProtoUDP {
		return nil, fmt.Errorf("unsupported protocol: 0x%02x", buf[1])
	}
	return &ConnectRequest{
		Version:  buf[0],
		Protocol: buf[1],
		DstIP:    net.IP(buf[2:6]).To4(),
		DstPort:  binary.BigEndian.Uint16(buf[6:8]),
	}, nil
}
