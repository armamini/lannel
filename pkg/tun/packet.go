package tun

import (
	"encoding/binary"
	"fmt"
	"net"
)

// IPVersion extracts the IP version from a raw packet.
func IPVersion(pkt []byte) int {
	if len(pkt) == 0 {
		return 0
	}
	return int(pkt[0] >> 4)
}

// IPv4Header represents a parsed IPv4 header.
type IPv4Header struct {
	Version    int
	IHL        int // header length in bytes
	TotalLen   int
	Protocol   int // 6=TCP, 17=UDP
	SrcIP      net.IP
	DstIP      net.IP
	Raw        []byte
	PayloadOff int // offset where transport header begins
}

const (
	ProtoTCP = 6
	ProtoUDP = 17
)

// ParseIPv4 parses an IPv4 header from a raw packet.
func ParseIPv4(pkt []byte) (*IPv4Header, error) {
	if len(pkt) < 20 {
		return nil, fmt.Errorf("packet too short: %d bytes", len(pkt))
	}

	version := int(pkt[0] >> 4)
	if version != 4 {
		return nil, fmt.Errorf("not IPv4: version %d", version)
	}

	ihl := int(pkt[0]&0x0f) * 4
	if ihl < 20 || len(pkt) < ihl {
		return nil, fmt.Errorf("invalid IHL: %d", ihl)
	}

	totalLen := int(binary.BigEndian.Uint16(pkt[2:4]))
	if totalLen > len(pkt) {
		totalLen = len(pkt)
	}

	return &IPv4Header{
		Version:    version,
		IHL:        ihl,
		TotalLen:   totalLen,
		Protocol:   int(pkt[9]),
		SrcIP:      net.IP(pkt[12:16]).To4(),
		DstIP:      net.IP(pkt[16:20]).To4(),
		Raw:        pkt[:totalLen],
		PayloadOff: ihl,
	}, nil
}

// TCPHeader represents key fields of a TCP header.
type TCPHeader struct {
	SrcPort  uint16
	DstPort  uint16
	SeqNum   uint32
	AckNum   uint32
	DataOff  int // header length in bytes
	Flags    uint8
	Window   uint16
	Checksum uint16
}

const (
	TCPFlagFIN = 0x01
	TCPFlagSYN = 0x02
	TCPFlagRST = 0x04
	TCPFlagACK = 0x10
)

// ParseTCP parses a TCP header from the transport payload.
func ParseTCP(data []byte) (*TCPHeader, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("TCP header too short: %d bytes", len(data))
	}

	dataOff := int((data[12] >> 4)) * 4
	if dataOff < 20 {
		dataOff = 20
	}

	return &TCPHeader{
		SrcPort:  binary.BigEndian.Uint16(data[0:2]),
		DstPort:  binary.BigEndian.Uint16(data[2:4]),
		SeqNum:   binary.BigEndian.Uint32(data[4:8]),
		AckNum:   binary.BigEndian.Uint32(data[8:12]),
		DataOff:  dataOff,
		Flags:    data[13],
		Window:   binary.BigEndian.Uint16(data[14:16]),
		Checksum: binary.BigEndian.Uint16(data[16:18]),
	}, nil
}

// UDPHeader represents a UDP header.
type UDPHeader struct {
	SrcPort uint16
	DstPort uint16
	Length  uint16
}

// ParseUDP parses a UDP header from the transport payload.
func ParseUDP(data []byte) (*UDPHeader, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("UDP header too short: %d bytes", len(data))
	}
	return &UDPHeader{
		SrcPort: binary.BigEndian.Uint16(data[0:2]),
		DstPort: binary.BigEndian.Uint16(data[2:4]),
		Length:  binary.BigEndian.Uint16(data[4:6]),
	}, nil
}
