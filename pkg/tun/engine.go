package tun

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/armamini/lannel/pkg/tunnel"
)

// Engine reads IP packets from a TUN device, identifies TCP/UDP flows,
// and forwards them through the binary tunnel protocol.
type Engine struct {
	dev    *Device
	tunnel *tunnel.Client

	// Track active TCP connections to avoid duplicate dials.
	tcpConns sync.Map // key: "srcIP:srcPort->dstIP:dstPort" -> *tcpFlow
}

type tcpFlow struct {
	conn   net.Conn
	cancel context.CancelFunc
}

// NewEngine creates a packet forwarding engine.
func NewEngine(dev *Device, tunnelClient *tunnel.Client) *Engine {
	return &Engine{
		dev:    dev,
		tunnel: tunnelClient,
	}
}

// Run starts the packet read loop. Blocks until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) error {
	buf := make([]byte, DefaultMTU+64) // extra headroom

	log.Printf("[Engine] Forwarding packets from %s via tunnel", e.dev.Name)

	for {
		select {
		case <-ctx.Done():
			e.closeAllFlows()
			return nil
		default:
		}

		n, err := e.dev.Read(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				log.Printf("[Engine] TUN read error: %v", err)
				continue
			}
		}

		if n == 0 {
			continue
		}

		pkt := make([]byte, n)
		copy(pkt, buf[:n])

		if IPVersion(pkt) != 4 {
			continue // skip IPv6 for now
		}

		ipHdr, err := ParseIPv4(pkt)
		if err != nil {
			continue
		}

		// Skip packets destined for the TUN subnet itself
		if ipHdr.DstIP.Equal(net.ParseIP(TunAddr)) || ipHdr.DstIP.Equal(net.ParseIP(TunGateway)) {
			continue
		}

		switch ipHdr.Protocol {
		case ProtoTCP:
			e.handleTCP(ctx, ipHdr)
		case ProtoUDP:
			e.handleUDP(ipHdr)
		}
	}
}

func (e *Engine) flowKey(srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16) string {
	return fmt.Sprintf("%s:%d->%s:%d", srcIP, srcPort, dstIP, dstPort)
}

func (e *Engine) handleTCP(ctx context.Context, ipHdr *IPv4Header) {
	transport := ipHdr.Raw[ipHdr.PayloadOff:]
	tcpHdr, err := ParseTCP(transport)
	if err != nil {
		return
	}

	key := e.flowKey(ipHdr.SrcIP, tcpHdr.SrcPort, ipHdr.DstIP, tcpHdr.DstPort)

	// On SYN: establish a new tunnel connection for this flow.
	if tcpHdr.Flags&TCPFlagSYN != 0 && tcpHdr.Flags&TCPFlagACK == 0 {
		// Close any stale flow
		if old, loaded := e.tcpConns.LoadAndDelete(key); loaded {
			f := old.(*tcpFlow)
			f.cancel()
			f.conn.Close()
		}

		go e.dialAndForwardTCP(ctx, key, ipHdr.DstIP, tcpHdr.DstPort)
		return
	}

	// On RST or FIN: clean up the flow
	if tcpHdr.Flags&TCPFlagRST != 0 || tcpHdr.Flags&TCPFlagFIN != 0 {
		if old, loaded := e.tcpConns.LoadAndDelete(key); loaded {
			f := old.(*tcpFlow)
			f.cancel()
			f.conn.Close()
		}
	}
}

func (e *Engine) dialAndForwardTCP(ctx context.Context, key string, dstIP net.IP, dstPort uint16) {
	conn, err := e.tunnel.DialTCP(dstIP, dstPort)
	if err != nil {
		log.Printf("[Engine] TCP tunnel dial %s:%d: %v", dstIP, dstPort, err)
		return
	}

	flowCtx, flowCancel := context.WithCancel(ctx)
	flow := &tcpFlow{conn: conn, cancel: flowCancel}
	e.tcpConns.Store(key, flow)

	go func() {
		<-flowCtx.Done()
		conn.Close()
	}()

	log.Printf("[Engine] TCP flow established: %s", key)
}

func (e *Engine) handleUDP(ipHdr *IPv4Header) {
	transport := ipHdr.Raw[ipHdr.PayloadOff:]
	udpHdr, err := ParseUDP(transport)
	if err != nil {
		return
	}

	// DNS (port 53) gets special fast-path handling via TCP tunnel
	if udpHdr.DstPort == 53 {
		go e.forwardDNS(ipHdr, transport)
		return
	}

	// Generic UDP: forward via tunnel
	go e.forwardUDPViaTunnel(ipHdr, udpHdr, transport)
}

func (e *Engine) forwardDNS(ipHdr *IPv4Header, transport []byte) {
	udpHdr, err := ParseUDP(transport)
	if err != nil {
		return
	}

	payload := transport[8:] // UDP header is 8 bytes
	if len(payload) == 0 {
		return
	}

	// Forward DNS query via TCP tunnel (DNS over TCP)
	conn, err := e.tunnel.DialTCP(ipHdr.DstIP, udpHdr.DstPort)
	if err != nil {
		log.Printf("[Engine] DNS tunnel dial %s: %v", ipHdr.DstIP, err)
		return
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// DNS over TCP: prepend 2-byte length
	tcpDNS := make([]byte, 2+len(payload))
	tcpDNS[0] = byte(len(payload) >> 8)
	tcpDNS[1] = byte(len(payload))
	copy(tcpDNS[2:], payload)

	if _, err := conn.Write(tcpDNS); err != nil {
		return
	}

	// Read response length
	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, lenBuf); err != nil {
		return
	}
	respLen := int(lenBuf[0])<<8 | int(lenBuf[1])
	if respLen > 65535 {
		return
	}

	resp := make([]byte, respLen)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return
	}

	e.injectUDPResponse(ipHdr.DstIP, udpHdr.DstPort, ipHdr.SrcIP, udpHdr.SrcPort, resp)
}

func (e *Engine) forwardUDPViaTunnel(ipHdr *IPv4Header, udpHdr *UDPHeader, transport []byte) {
	payload := transport[8:]
	if len(payload) == 0 {
		return
	}

	conn, err := e.tunnel.DialUDP(ipHdr.DstIP, udpHdr.DstPort)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write(payload); err != nil {
		return
	}

	resp := make([]byte, DefaultMTU)
	n, err := conn.Read(resp)
	if err != nil {
		return
	}

	e.injectUDPResponse(ipHdr.DstIP, udpHdr.DstPort, ipHdr.SrcIP, udpHdr.SrcPort, resp[:n])
}

// injectUDPResponse crafts a raw IPv4+UDP packet and writes it to the TUN,
// delivering the response back to the original requesting application.
func (e *Engine) injectUDPResponse(srcIP net.IP, srcPort uint16, dstIP net.IP, dstPort uint16, payload []byte) {
	udpLen := 8 + len(payload)
	totalLen := 20 + udpLen

	pkt := make([]byte, totalLen)

	// IPv4 header
	pkt[0] = 0x45 // version 4, IHL 5
	pkt[1] = 0x00 // DSCP/ECN
	pkt[2] = byte(totalLen >> 8)
	pkt[3] = byte(totalLen)
	pkt[4] = 0x00 // identification
	pkt[5] = 0x00
	pkt[6] = 0x40 // flags: Don't Fragment
	pkt[7] = 0x00
	pkt[8] = 64 // TTL
	pkt[9] = ProtoUDP
	// checksum at [10:12] — computed below
	copy(pkt[12:16], srcIP.To4())
	copy(pkt[16:20], dstIP.To4())

	// IPv4 header checksum
	var csum uint32
	for i := 0; i < 20; i += 2 {
		csum += uint32(pkt[i])<<8 | uint32(pkt[i+1])
	}
	for csum > 0xffff {
		csum = (csum & 0xffff) + (csum >> 16)
	}
	cs := ^uint16(csum)
	pkt[10] = byte(cs >> 8)
	pkt[11] = byte(cs)

	// UDP header
	udp := pkt[20:]
	udp[0] = byte(srcPort >> 8)
	udp[1] = byte(srcPort)
	udp[2] = byte(dstPort >> 8)
	udp[3] = byte(dstPort)
	udp[4] = byte(udpLen >> 8)
	udp[5] = byte(udpLen)
	udp[6] = 0 // checksum (optional for IPv4)
	udp[7] = 0
	copy(udp[8:], payload)

	if _, err := e.dev.Write(pkt); err != nil {
		log.Printf("[Engine] TUN write error: %v", err)
	}
}

func (e *Engine) closeAllFlows() {
	e.tcpConns.Range(func(key, value any) bool {
		f := value.(*tcpFlow)
		f.cancel()
		f.conn.Close()
		e.tcpConns.Delete(key)
		return true
	})
}
