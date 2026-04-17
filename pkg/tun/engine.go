package tun

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// Engine reads IP packets from a TUN device, identifies TCP/UDP flows,
// and forwards them through a SOCKS5 proxy.
type Engine struct {
	dev       *Device
	proxyAddr string

	// Track active TCP connections to avoid duplicate dials.
	tcpConns sync.Map // key: "srcIP:srcPort->dstIP:dstPort" -> *tcpFlow
}

type tcpFlow struct {
	proxy  net.Conn
	cancel context.CancelFunc
}

// NewEngine creates a packet forwarding engine.
func NewEngine(dev *Device, proxyAddr string) *Engine {
	return &Engine{
		dev:       dev,
		proxyAddr: proxyAddr,
	}
}

// Run starts the packet read loop. Blocks until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) error {
	buf := make([]byte, DefaultMTU+64) // extra headroom

	log.Printf("[Engine] Forwarding packets from %s → SOCKS5 %s", e.dev.Name, e.proxyAddr)

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
			e.handleUDP(ctx, ipHdr)
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

	// On SYN: establish a new SOCKS5 connection for this flow.
	if tcpHdr.Flags&TCPFlagSYN != 0 && tcpHdr.Flags&TCPFlagACK == 0 {
		// Close any stale flow
		if old, loaded := e.tcpConns.LoadAndDelete(key); loaded {
			f := old.(*tcpFlow)
			f.cancel()
			f.proxy.Close()
		}

		go e.dialAndForwardTCP(ctx, key, ipHdr.DstIP, tcpHdr.DstPort)
		return
	}

	// On RST or FIN: clean up the flow
	if tcpHdr.Flags&TCPFlagRST != 0 || tcpHdr.Flags&TCPFlagFIN != 0 {
		if old, loaded := e.tcpConns.LoadAndDelete(key); loaded {
			f := old.(*tcpFlow)
			f.cancel()
			f.proxy.Close()
		}
	}
}

func (e *Engine) dialAndForwardTCP(ctx context.Context, key string, dstIP net.IP, dstPort uint16) {
	proxyConn, err := DialSOCKS5(e.proxyAddr, dstIP, dstPort)
	if err != nil {
		log.Printf("[Engine] TCP dial %s:%d via SOCKS5: %v", dstIP, dstPort, err)
		return
	}

	flowCtx, flowCancel := context.WithCancel(ctx)
	flow := &tcpFlow{proxy: proxyConn, cancel: flowCancel}
	e.tcpConns.Store(key, flow)

	// When context ends, close the connection
	go func() {
		<-flowCtx.Done()
		proxyConn.Close()
	}()

	log.Printf("[Engine] TCP flow established: %s", key)
}

func (e *Engine) handleUDP(ctx context.Context, ipHdr *IPv4Header) {
	transport := ipHdr.Raw[ipHdr.PayloadOff:]
	udpHdr, err := ParseUDP(transport)
	if err != nil {
		return
	}

	// DNS (port 53) gets special fast-path handling
	if udpHdr.DstPort == 53 {
		go e.forwardDNS(ipHdr, transport)
		return
	}

	// Generic UDP: forward via direct connection (SOCKS5 UDP associate
	// is often unreliable; direct UDP works when routing is through the proxy)
	go e.forwardUDPDirect(ipHdr, udpHdr, transport)
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

	// Forward DNS query via TCP through SOCKS5 (DNS over TCP)
	proxyConn, err := DialSOCKS5(e.proxyAddr, ipHdr.DstIP, udpHdr.DstPort)
	if err != nil {
		log.Printf("[Engine] DNS dial %s: %v", ipHdr.DstIP, err)
		return
	}
	defer proxyConn.Close()

	proxyConn.SetDeadline(time.Now().Add(10 * time.Second))

	// DNS over TCP: prepend 2-byte length
	tcpDNS := make([]byte, 2+len(payload))
	tcpDNS[0] = byte(len(payload) >> 8)
	tcpDNS[1] = byte(len(payload))
	copy(tcpDNS[2:], payload)

	if _, err := proxyConn.Write(tcpDNS); err != nil {
		return
	}

	// Read response length
	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(proxyConn, lenBuf); err != nil {
		return
	}
	respLen := int(lenBuf[0])<<8 | int(lenBuf[1])
	if respLen > 65535 {
		return
	}

	resp := make([]byte, respLen)
	if _, err := io.ReadFull(proxyConn, resp); err != nil {
		return
	}

	// We've resolved DNS. The response would need to be injected back
	// into the TUN as a UDP packet. This requires crafting a raw IP+UDP
	// response packet — handled by the response injector.
	e.injectUDPResponse(ipHdr.DstIP, udpHdr.DstPort, ipHdr.SrcIP, udpHdr.SrcPort, resp)
}

func (e *Engine) forwardUDPDirect(ipHdr *IPv4Header, udpHdr *UDPHeader, transport []byte) {
	payload := transport[8:]
	if len(payload) == 0 {
		return
	}

	dst := net.JoinHostPort(ipHdr.DstIP.String(), fmt.Sprintf("%d", udpHdr.DstPort))
	conn, err := net.DialTimeout("udp", dst, 5*time.Second)
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
		f.proxy.Close()
		e.tcpConns.Delete(key)
		return true
	})
}
