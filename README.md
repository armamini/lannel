# LANnel

**Local Network VPN/Proxy Gateway**

LANnel lets a **Server** machine share its internet connection — including any active VPN tunnel — with **Client** machines on the same LAN. The server exposes a SOCKS5 proxy that follows the host OS's default route, so traffic from connected clients automatically flows through whatever VPN (Windscribe, NordVPN, Nekoray, etc.) is running on the server. The client operates at Layer 3 using a virtual TUN interface, capturing **all** system traffic — not just browser traffic.

```
┌──────────────────────┐          LAN          ┌──────────────────────┐
│     Client (B)       │                       │     Server (A)       │
│                      │                       │                      │
│  ┌────────────────┐  │                       │  ┌────────────────┐  │
│  │  Applications  │  │                       │  │  SOCKS5 Proxy  │──┼──► OS Default Route
│  │  (all traffic) │  │                       │  │   :1080        │  │    (VPN if active)
│  └───────┬────────┘  │                       │  └────────────────┘  │
│          │           │                       │                      │
│  ┌───────▼────────┐  │    SOCKS5 over TCP    │  ┌────────────────┐  │
│  │  TUN Interface │  ├───────────────────────►  │   Web UI       │  │
│  │  (tun0/utun)   │  │                       │  │   :8080        │  │
│  └───────┬────────┘  │                       │  └────────────────┘  │
│          │           │                       │                      │
│  ┌───────▼────────┐  │                       │                      │
│  │ Packet Engine  │  │                       │                      │
│  │ L3 → SOCKS5    │  │                       │                      │
│  └────────────────┘  │                       │                      │
└──────────────────────┘                       └──────────────────────┘
```

---

## Features

- **VPN-Transparent Proxy** — Server's SOCKS5 proxy does not bind to any specific interface. If a VPN is active on the server, all proxied traffic automatically routes through it.
- **System-Wide Tunnel** — Client creates a TUN interface and reroutes the OS default gateway, capturing all TCP/UDP traffic from every application.
- **Web Dashboard** — Beautiful onboarding UI with auto-detected LAN IP, QR code for mobile proxy apps, and manual setup instructions.
- **DNS Leak Prevention** — DNS queries are forwarded through the SOCKS5 tunnel as DNS-over-TCP.
- **Graceful Shutdown** — Client catches `SIGINT`/`SIGTERM` and restores the original routing table before exiting.
- **Cross-Platform** — Builds for Linux, macOS, and Windows. Zero CGO. Single static binary per component.
- **LAN Access Control** — Optional CIDR-based restriction on the SOCKS5 proxy (e.g., allow only `192.168.1.0/24`).

---

## Architecture

### Server Component

The server runs two concurrent services:

| Service | Default Port | Description |
|---------|-------------|-------------|
| **SOCKS5 Proxy** | `1080` | Accepts SOCKS5 connections and forwards traffic through the OS default route |
| **Web UI** | `8080` | HTTP dashboard for onboarding and connection details |

**Key design:** The proxy intentionally avoids binding outbound connections to a physical NIC. This means the OS routing table decides where traffic goes — if a VPN client (NordVPN, Windscribe, Nekoray, etc.) has modified the default route, proxy traffic flows through the VPN tunnel automatically.

### Client Component

The client performs three operations:

1. **TUN Creation** — Creates a virtual network interface (`tun0` on Linux, `utunN` on macOS, TAP adapter on Windows) using the `water` library.
2. **Route Hijacking** — Adds two covering routes (`0.0.0.0/1` + `128.0.0.0/1`) that are more specific than the default `0.0.0.0/0` route, effectively capturing all traffic without destroying the original default route. A static `/32` bypass route is added for the server's IP to prevent routing loops.
3. **Packet Forwarding** — Reads raw IPv4 packets from the TUN device, parses IP/TCP/UDP headers, and forwards flows through the SOCKS5 proxy.

### Project Structure

```
lannel/
├── cmd/
│   ├── server/
│   │   └── main.go              # Server entry point
│   └── client/
│       └── main.go              # Client entry point
├── pkg/
│   ├── proxy/
│   │   └── proxy.go             # SOCKS5 server (go-socks5 wrapper)
│   ├── web/
│   │   ├── web.go               # HTTP server + HTML template
│   │   ├── netutil.go           # LAN IP auto-detection
│   │   └── qr.go                # QR code generation
│   └── tun/
│       ├── tun.go               # TUN device lifecycle
│       ├── engine.go            # Packet read loop + SOCKS5 forwarding
│       ├── packet.go            # IPv4/TCP/UDP header parsing
│       ├── socks.go             # SOCKS5 client dialer
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
go install -v github.com/armamini/lannel/cmd/server@latest

# Install the client
go install -v github.com/armamini/lannel/cmd/client@latest
```

Binaries are placed in `$GOPATH/bin` (or `$HOME/go/bin` by default). Make sure it's in your `PATH`.

### Build from Source

```bash
git clone https://github.com/armamini/lannel.git
cd lannel

# Build both binaries
go build -o lannel-server ./cmd/server
go build -o lannel-client ./cmd/client
```

### Cross-Compile

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o lannel-server-linux ./cmd/server
GOOS=linux GOARCH=amd64 go build -o lannel-client-linux ./cmd/client

# Windows
GOOS=windows GOARCH=amd64 go build -o lannel-server.exe ./cmd/server
GOOS=windows GOARCH=amd64 go build -o lannel-client.exe ./cmd/client

# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o lannel-server-darwin ./cmd/server
GOOS=darwin GOARCH=arm64 go build -o lannel-client-darwin ./cmd/client
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
[LANnel Server] Started (SOCKS5 :1080 | Web UI :8080)
[SOCKS5] Listening on 0.0.0.0:1080
[Web UI] Listening on http://192.168.1.10:8080
```

Open `http://192.168.1.10:8080` in a browser to see the dashboard.

#### Server Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--bind` | `0.0.0.0` | Bind address for both services |
| `--socks-port` | `1080` | SOCKS5 listen port |
| `--http-port` | `8080` | Web UI listen port |
| `--allowed-subnet` | *(empty)* | Restrict proxy access to a CIDR (e.g., `192.168.1.0/24`) |

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
[LANnel Client] Target SOCKS5 proxy: 192.168.1.10:1080
[TUN] Created interface: tun0
[TUN] Original gateway: 192.168.1.1 via eth0
[TUN] Routes configured — all traffic routed through tun0
[Engine] Forwarding packets from tun0 → SOCKS5 192.168.1.10:1080
[LANnel Client] System-wide tunnel active. Press Ctrl+C to stop.
```

Press `Ctrl+C` to disconnect. The original routing table is restored automatically.

##### Client Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-server` | *(required)* | Server's LAN IP address |
| `-port` | `1080` | Server's SOCKS5 port |

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
Client App → TUN (tun0) → Packet Engine → SOCKS5 (TCP) → Server OS Route → VPN Tunnel → Internet
```

1. The **client** captures all outbound packets via the TUN interface
2. Packets are parsed and translated into SOCKS5 `CONNECT` requests
3. The **server's** SOCKS5 proxy opens an outbound connection using `net.Dial` — which follows the OS routing table
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

- **No authentication** — The SOCKS5 proxy has no username/password by default. Use `--allowed-subnet` to restrict access to trusted LAN ranges.
- **No encryption** — Traffic between client and server is unencrypted on the LAN. This is acceptable for trusted local networks. For untrusted networks, layer SSH tunneling on top.
- **Root required** — The client needs root to create TUN interfaces and modify routes. The server does **not** require root (unless binding to ports below 1024).
- **DNS privacy** — DNS queries are forwarded as DNS-over-TCP through the SOCKS5 tunnel, preventing DNS leaks to the local network's resolver.

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
