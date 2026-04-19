# LANnel

**Local Network VPN/Proxy Gateway**

LANnel lets a **Server** machine share its internet connection — including any active VPN tunnel — with **Client** machines on the same LAN. The server runs a high-performance binary tunnel (for the CLI client) and a SOCKS5 proxy (for browsers, mobile apps, and manual configuration), both following the host OS's default route. Traffic from connected clients automatically flows through whatever VPN (Windscribe, NordVPN, Nekoray, etc.) is running on the server. The CLI client operates at Layer 3 using a virtual TUN interface, capturing **all** system traffic — not just browser traffic.

```
┌──────────────────────┐          LAN          ┌──────────────────────┐
│     Client (B)       │                       │     Server (A)       │
│                      │                       │                      │
│  ┌────────────────┐  │                       │  ┌────────────────┐  │
│  │  Applications  │  │                       │  │ Binary Tunnel  │──┼──► OS Default Route
│  │  (all traffic) │  │                       │  │   :9090        │  │    (VPN if active)
│  └───────┬────────┘  │                       │  └────────────────┘  │
│          │           │                       │                      │
│  ┌───────▼────────┐  │  Binary Tunnel (TCP)  │  ┌────────────────┐  │
│  │  TUN Interface │  ├───────────────────────►  │  SOCKS5 Proxy  │──┼──► OS Default Route
│  │  (tun0/utun)   │  │                       │  │   :1080        │  │    (browsers/mobile)
│  └───────┬────────┘  │                       │  └────────────────┘  │
│          │           │                       │                      │
│  ┌───────▼────────┐  │                       │  ┌────────────────┐  │
│  │ Packet Engine  │  │                       │  │   Web UI       │  │
│  │ L3 → Tunnel    │  │                       │  │   :8080        │  │
│  └────────────────┘  │                       │  └────────────────┘  │
└──────────────────────┘                       └──────────────────────┘
```

---

## Features

- **Binary Tunnel Protocol** — CLI client communicates via a minimal 8-byte binary handshake followed by zero-overhead bidirectional relay — significantly lower latency than SOCKS5.
- **VPN-Transparent Proxy** — Neither the tunnel server nor the SOCKS5 proxy binds to any specific interface. If a VPN is active on the server, all proxied traffic automatically routes through it.
- **System-Wide Tunnel** — Client creates a TUN interface and reroutes the OS default gateway, capturing all TCP/UDP traffic from every application.
- **SOCKS5 for Manual Use** — Browsers, mobile apps (via QR code), and CLI tools like `curl` can still connect directly through the SOCKS5 proxy.
- **Web Dashboard** — Beautiful onboarding UI with auto-detected LAN IP, QR code for mobile proxy apps, and manual setup instructions.
- **DNS Leak Prevention** — DNS queries are forwarded through the tunnel as DNS-over-TCP.
- **Graceful Shutdown** — Client catches `SIGINT`/`SIGTERM` and restores the original routing table before exiting.
- **Cross-Platform** — Builds for Linux, macOS, and Windows. Zero CGO. Single static binary per component.
- **LAN Access Control** — Optional CIDR-based restriction on the SOCKS5 proxy (e.g., allow only `192.168.1.0/24`).

---

## Architecture

### Server Component

The server runs three concurrent services:

| Service | Default Port | Description |
|---------|-------------|-------------|
| **Binary Tunnel** | `9090` | Accepts binary tunnel connections from CLI clients — minimal handshake, zero-framing data relay |
| **SOCKS5 Proxy** | `1080` | Accepts SOCKS5 connections for browsers, mobile apps, and manual proxy configuration |
| **Web UI** | `8080` | HTTP dashboard for onboarding and connection details |

**Key design:** Both the tunnel and proxy intentionally avoid binding outbound connections to a physical NIC. This means the OS routing table decides where traffic goes — if a VPN client (NordVPN, Windscribe, Nekoray, etc.) has modified the default route, traffic flows through the VPN tunnel automatically.

### Client Component

The client performs three operations:

1. **TUN Creation** — Creates a virtual network interface (`tun0` on Linux, `utunN` on macOS, TAP adapter on Windows) using the `water` library.
2. **Route Hijacking** — Adds two covering routes (`0.0.0.0/1` + `128.0.0.0/1`) that are more specific than the default `0.0.0.0/0` route, effectively capturing all traffic without destroying the original default route. A static `/32` bypass route is added for the server's IP to prevent routing loops.
3. **Packet Forwarding** — Reads raw IPv4 packets from the TUN device, parses IP/TCP/UDP headers, and forwards flows through the binary tunnel protocol.

### Project Structure

```
lannel/
├── cmd/
│   ├── lannel-server/
│   │   └── main.go              # Server entry point
│   └── lannel-client/
│       └── main.go              # Client entry point
├── pkg/
│   ├── proxy/
│   │   └── proxy.go             # SOCKS5 server (go-socks5 wrapper)
│   ├── tunnel/
│   │   ├── protocol.go          # Binary wire protocol (8-byte header)
│   │   ├── server.go            # Tunnel server: accept, relay TCP/UDP
│   │   └── client.go            # Tunnel client: dial through server
│   ├── web/
│   │   ├── web.go               # HTTP server + HTML template
│   │   ├── netutil.go           # LAN IP auto-detection
│   │   └── qr.go                # QR code generation
│   └── tun/
│       ├── tun.go               # TUN device lifecycle
│       ├── engine.go            # Packet read loop + tunnel forwarding
│       ├── packet.go            # IPv4/TCP/UDP header parsing
│       ├── route_linux.go       # Linux routing (ip route)
│       ├── route_darwin.go      # macOS routing (route)
│       └── route_windows.go     # Windows routing (route/netsh)
├── go.mod
└── go.sum
```

---

## Prerequisites

- **Go 1.21+**
- **Root/Administrator privileges** (client only — required for TUN creation and routing table modification)
- Server and client must be on the **same LAN**

---

## Installation

### Go Install (Recommended)

```bash
# Install the server
go install -v github.com/armamini/lannel/cmd/lannel-server@latest

# Install the client
go install -v github.com/armamini/lannel/cmd/lannel-client@latest
```

Binaries are placed in `$GOPATH/bin` (or `$HOME/go/bin` by default). Make sure it's in your `PATH`.

### Build from Source

```bash
git clone https://github.com/armamini/lannel.git
cd lannel

# Build both binaries
go build -o lannel-server ./cmd/lannel-server
go build -o lannel-client ./cmd/lannel-client
```

### Cross-Compile

```bash
# Linux (amd64)
GOOS=linux GOARCH=amd64 go build -o lannel-server-linux ./cmd/lannel-server
GOOS=linux GOARCH=amd64 go build -o lannel-client-linux ./cmd/lannel-client

# Windows (amd64)
GOOS=windows GOARCH=amd64 go build -o lannel-server.exe ./cmd/lannel-server
GOOS=windows GOARCH=amd64 go build -o lannel-client.exe ./cmd/lannel-client

# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o lannel-server-darwin ./cmd/lannel-server
GOOS=darwin GOARCH=arm64 go build -o lannel-client-darwin ./cmd/lannel-client
```

---

## Usage

### 1. Start the Server (Machine A)

This is the machine that has internet access and/or an active VPN.

```bash
./lannel-server
```

Output:
```
[LANnel Server] Started (SOCKS5 :1080 | Tunnel :9090 | Web UI :8080)
[SOCKS5] Listening on 0.0.0.0:1080
[Tunnel] Listening on 0.0.0.0:9090
[Web UI] Listening on http://192.168.1.10:8080
```

Open `http://192.168.1.10:8080` in a browser to see the dashboard.

#### Server Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--bind` | `0.0.0.0` | Bind address for all services |
| `--socks-port` | `1080` | SOCKS5 listen port (browsers/mobile/manual) |
| `--tunnel-port` | `9090` | Binary tunnel listen port (CLI client) |
| `--http-port` | `8080` | Web UI listen port |
| `--allowed-subnet` | *(empty)* | Restrict SOCKS5 access to a CIDR (e.g., `192.168.1.0/24`) |

#### Example: Restrict to LAN Only

```bash
./lannel-server --allowed-subnet 192.168.1.0/24
```

### 2. Connect a Client (Machine B)

#### Option A: System-Wide Tunnel (CLI Client)

Routes **all** system traffic through the server. Requires root.

```bash
sudo ./lannel-client -server 192.168.1.10
```

Output:
```
[LANnel Client] Target tunnel server: 192.168.1.10:9090
[TUN] Created interface: tun0
[TUN] Original gateway: 192.168.1.1 via eth0
[TUN] Routes configured — all traffic routed through tun0
[Engine] Forwarding packets from tun0 via tunnel
[LANnel Client] System-wide tunnel active. Press Ctrl+C to stop.
```

Press `Ctrl+C` to disconnect. The original routing table is restored automatically.

##### Client Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-server` | *(required)* | Server's LAN IP address |
| `-port` | `9090` | Server's tunnel port |

#### Option B: Manual SOCKS5 (Browser/App)

Configure any application's proxy settings:

| Field | Value |
|-------|-------|
| Protocol | SOCKS5 |
| Host | `192.168.1.10` |
| Port | `1080` |
| Authentication | None |

**Firefox:** Settings > Network Settings > Manual Proxy > SOCKS Host

**Chrome (CLI):**
```bash
google-chrome --proxy-server="socks5://192.168.1.10:1080"
```

**curl:**
```bash
curl --socks5 192.168.1.10:1080 https://ifconfig.me
```

#### Option C: Mobile (QR Code)

1. Open `http://192.168.1.10:8080` on the server or any LAN device
2. Scan the QR code with **v2rayNG** (Android) or **Shadowrocket** (iOS)
3. The QR encodes `socks5://192.168.1.10:1080` — connect and route traffic through the server

---

## How It Works with VPNs

```
Client App → TUN (tun0) → Packet Engine → Binary Tunnel (TCP) → Server OS Route → VPN Tunnel → Internet
```

1. The **client** captures all outbound packets via the TUN interface
2. Packets are parsed and forwarded through the binary tunnel protocol (8-byte handshake, then raw bidirectional relay)
3. The **server's** tunnel opens an outbound connection using `net.Dial` — which follows the OS routing table
4. If a VPN client is active on the server, the OS routes traffic through the VPN's virtual adapter automatically
5. **No configuration changes needed** — start your VPN, start LANnel, done

### Routing Topology

```
Client routing table (after lannel-client):
  192.168.1.10/32  via 192.168.1.1 dev eth0     ← Server bypass (prevents loop)
  0.0.0.0/1        dev tun0                      ← Captures 0.x–127.x
  128.0.0.0/1      dev tun0                      ← Captures 128.x–255.x
  0.0.0.0/0        via 192.168.1.1 dev eth0      ← Original default (lower priority)
```

The two `/1` routes are more specific than the `/0` default, so they win for all destinations **except** the server's own IP (which has a `/32` override).

---

## Security Considerations

- **No authentication** — Neither the tunnel nor the SOCKS5 proxy has authentication by default. Use `--allowed-subnet` to restrict SOCKS5 access to trusted LAN ranges.
- **No encryption** — Traffic between client and server is unencrypted on the LAN. This is acceptable for trusted local networks. For untrusted networks, layer SSH tunneling on top.
- **Root required** — The client needs root to create TUN interfaces and modify routes. The server does **not** require root (unless binding to ports below 1024).
- **DNS privacy** — DNS queries are forwarded as DNS-over-TCP through the tunnel, preventing DNS leaks to the local network's resolver.

---

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `TUN creation failed` | Ensure you're running the client with `sudo` / as Administrator |
| `Route setup failed` | Check that no other VPN client on the **client** machine is managing routes |
| `SOCKS5 connect failed` | Verify the server is reachable: `nc -z <server-ip> 1080` |
| Client loses connectivity after `Ctrl+C` | Routes should auto-restore. If not, manually reset: `sudo ip route del 0.0.0.0/1; sudo ip route del 128.0.0.0/1` (Linux) |
| Port already in use | Change ports with `--socks-port` / `--http-port` on the server |

---

## License

MIT
