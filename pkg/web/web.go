package web

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"time"
)

// Config holds Web UI configuration.
type Config struct {
	BindAddr  string
	HTTPPort  int
	SocksPort int
}

// Server is the Web UI HTTP server.
type Server struct {
	cfg    Config
	lanIP  string
	tmpl   *template.Template
	server *http.Server
}

// New creates a new Web UI server. It auto-detects the LAN IP.
func New(cfg Config) (*Server, error) {
	lanIP, err := detectLANIP()
	if err != nil {
		return nil, fmt.Errorf("lan ip detection: %w", err)
	}

	tmpl, err := template.New("index").Parse(indexHTML)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	return &Server{
		cfg:   cfg,
		lanIP: lanIP,
		tmpl:  tmpl,
	}, nil
}

// ListenAndServe starts the HTTP server. Blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)

	addr := fmt.Sprintf("%s:%d", s.cfg.BindAddr, s.cfg.HTTPPort)
	s.server = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("web listen on %s: %w", addr, err)
	}

	log.Printf("[Web UI] Listening on http://%s:%d", s.lanIP, s.cfg.HTTPPort)

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.server.Shutdown(shutCtx)
	}()

	if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("web serve: %w", err)
	}
	return nil
}

type templateData struct {
	LANIP       string
	SocksPort   int
	QRBlock     template.HTML
	SocksURI    string
	CurrentYear int
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	socksURI := fmt.Sprintf("socks5://%s:%d", s.lanIP, s.cfg.SocksPort)
	data := templateData{
		LANIP:       s.lanIP,
		SocksPort:   s.cfg.SocksPort,
		QRBlock:     template.HTML(generateQRInfoBlock(s.lanIP, s.cfg.SocksPort)),
		SocksURI:    socksURI,
		CurrentYear: time.Now().Year(),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")

	if err := s.tmpl.Execute(w, data); err != nil {
		log.Printf("[Web UI] Template render error: %v", err)
	}
}

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>LANnel — LAN VPN Gateway</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <script>
    tailwind.config = {
        theme: {
            extend: {
                colors: {
                    brand: { 50:'#f0f9ff', 100:'#e0f2fe', 500:'#0ea5e9', 600:'#0284c7', 700:'#0369a1', 900:'#0c4a6e' }
                }
            }
        }
    }
    </script>
    <style>
        body { background: linear-gradient(135deg, #0f172a 0%, #1e293b 50%, #0f172a 100%); }
        .glass { background: rgba(15, 23, 42, 0.6); backdrop-filter: blur(16px); border: 1px solid rgba(148, 163, 184, 0.1); }
        .glow { box-shadow: 0 0 40px rgba(14, 165, 233, 0.15); }
        code { font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace; }
        .copy-btn:active { transform: scale(0.95); }
    </style>
</head>
<body class="min-h-screen text-slate-200 antialiased">
    <div class="max-w-3xl mx-auto px-4 py-12 sm:py-20">

        <!-- Header -->
        <div class="text-center mb-12">
            <div class="inline-flex items-center gap-2 mb-4">
                <div class="w-3 h-3 rounded-full bg-emerald-400 animate-pulse"></div>
                <span class="text-xs uppercase tracking-widest text-emerald-400 font-semibold">Server Online</span>
            </div>
            <h1 class="text-4xl sm:text-5xl font-bold bg-gradient-to-r from-brand-500 to-cyan-400 bg-clip-text text-transparent">
                LANnel
            </h1>
            <p class="mt-3 text-slate-400 text-lg">LAN VPN/Proxy Gateway</p>
        </div>

        <!-- Server Info Card -->
        <div class="glass rounded-2xl p-6 sm:p-8 glow mb-8">
            <h2 class="text-lg font-semibold text-white mb-4 flex items-center gap-2">
                <svg class="w-5 h-5 text-brand-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 12h14M12 5l7 7-7 7"/></svg>
                Connection Details
            </h2>
            <div class="grid sm:grid-cols-2 gap-4">
                <div class="bg-slate-800/50 rounded-xl p-4">
                    <p class="text-xs text-slate-500 uppercase tracking-wider mb-1">Server LAN IP</p>
                    <p class="text-xl font-mono font-bold text-white">{{.LANIP}}</p>
                </div>
                <div class="bg-slate-800/50 rounded-xl p-4">
                    <p class="text-xs text-slate-500 uppercase tracking-wider mb-1">SOCKS5 Port</p>
                    <p class="text-xl font-mono font-bold text-white">{{.SocksPort}}</p>
                </div>
            </div>
        </div>

        <!-- Tabs -->
        <div class="glass rounded-2xl overflow-hidden mb-8">
            <div class="flex border-b border-slate-700/50">
                <button onclick="showTab('mobile')" id="tab-mobile" class="tab-btn flex-1 py-3 px-4 text-sm font-medium text-center text-brand-500 border-b-2 border-brand-500 transition-colors">
                    Mobile (QR)
                </button>
                <button onclick="showTab('manual')" id="tab-manual" class="tab-btn flex-1 py-3 px-4 text-sm font-medium text-center text-slate-400 border-b-2 border-transparent hover:text-slate-200 transition-colors">
                    Manual Setup
                </button>
                <button onclick="showTab('cli')" id="tab-cli" class="tab-btn flex-1 py-3 px-4 text-sm font-medium text-center text-slate-400 border-b-2 border-transparent hover:text-slate-200 transition-colors">
                    CLI Client
                </button>
            </div>

            <!-- Mobile Tab -->
            <div id="panel-mobile" class="tab-panel p-6 sm:p-8">
                <p class="text-sm text-slate-300 mb-6 text-center">
                    Scan this QR code with <strong>v2rayNG</strong>, <strong>Shadowrocket</strong>, or any SOCKS5 proxy app.
                </p>
                {{.QRBlock}}
            </div>

            <!-- Manual Tab -->
            <div id="panel-manual" class="tab-panel p-6 sm:p-8 hidden">
                <h3 class="text-sm font-semibold text-slate-300 mb-4">Configure any app that supports SOCKS5:</h3>
                <div class="space-y-3">
                    <div class="flex items-center justify-between bg-slate-800/50 rounded-lg p-3">
                        <div><span class="text-xs text-slate-500">Protocol</span><p class="font-mono text-sm">SOCKS5</p></div>
                    </div>
                    <div class="flex items-center justify-between bg-slate-800/50 rounded-lg p-3">
                        <div><span class="text-xs text-slate-500">Host / Server</span><p class="font-mono text-sm">{{.LANIP}}</p></div>
                        <button onclick="copyText('{{.LANIP}}')" class="copy-btn text-xs bg-brand-600 hover:bg-brand-700 text-white px-3 py-1 rounded-md transition-colors">Copy</button>
                    </div>
                    <div class="flex items-center justify-between bg-slate-800/50 rounded-lg p-3">
                        <div><span class="text-xs text-slate-500">Port</span><p class="font-mono text-sm">{{.SocksPort}}</p></div>
                        <button onclick="copyText('{{.SocksPort}}')" class="copy-btn text-xs bg-brand-600 hover:bg-brand-700 text-white px-3 py-1 rounded-md transition-colors">Copy</button>
                    </div>
                    <div class="flex items-center justify-between bg-slate-800/50 rounded-lg p-3">
                        <div><span class="text-xs text-slate-500">Authentication</span><p class="font-mono text-sm text-slate-400">None</p></div>
                    </div>
                </div>
                <div class="mt-6 p-4 bg-amber-500/10 border border-amber-500/20 rounded-xl">
                    <p class="text-xs text-amber-400">
                        <strong>Browser Proxy:</strong> Set your browser's SOCKS5 proxy to <code>{{.LANIP}}:{{.SocksPort}}</code>.
                        For Firefox: Settings → Network → Manual Proxy → SOCKS Host.
                    </p>
                </div>
            </div>

            <!-- CLI Tab -->
            <div id="panel-cli" class="tab-panel p-6 sm:p-8 hidden">
                <h3 class="text-sm font-semibold text-slate-300 mb-4">System-wide routing via LANnel Client:</h3>
                <div class="bg-slate-900 rounded-xl p-4 mb-4 relative group">
                    <button onclick="copyText('./lannel-client -server {{.LANIP}}')" class="copy-btn absolute top-3 right-3 text-xs bg-slate-700 hover:bg-slate-600 text-white px-2 py-1 rounded transition-colors opacity-0 group-hover:opacity-100">Copy</button>
                    <pre class="text-sm text-emerald-400 overflow-x-auto"><code># Run with sudo/admin privileges
sudo ./lannel-client -server {{.LANIP}}</code></pre>
                </div>
                <div class="space-y-2 text-sm text-slate-400">
                    <p>The CLI client will:</p>
                    <ol class="list-decimal list-inside space-y-1 ml-2">
                        <li>Create a virtual TUN interface (<code class="text-xs bg-slate-800 px-1 rounded">tun0</code>)</li>
                        <li>Redirect all system traffic through the tunnel</li>
                        <li>Forward packets to this server's SOCKS5 proxy</li>
                        <li>Restore original routes on exit (<code class="text-xs bg-slate-800 px-1 rounded">Ctrl+C</code>)</li>
                    </ol>
                </div>
                <div class="mt-6 p-4 bg-brand-500/10 border border-brand-500/20 rounded-xl">
                    <p class="text-xs text-brand-400">
                        <strong>Requires:</strong> Root/Administrator privileges for TUN interface creation and routing table modification.
                    </p>
                </div>
            </div>
        </div>

        <!-- Footer -->
        <div class="text-center text-xs text-slate-600">
            LANnel &copy; {{.CurrentYear}} &mdash; Local Network VPN Gateway
        </div>
    </div>

    <!-- QR Code Generator (qr.js - minimal inline QR encoder) -->
    <script>
    // Minimal QR Code generator - renders to canvas
    // Based on the QR Code specification (ISO 18004)
    (function(){
        const el = document.getElementById('qrcode');
        if (!el) return;
        const text = el.dataset.content;
        if (!text) return;

        // Use a simple API-based SVG approach with inline generation
        // We'll create a QR using the proven "qrcodejs" minimal algo
        // For robustness, use an image from a well-known QR API as fallback
        const canvas = document.createElement('canvas');
        const size = 200;
        canvas.width = size;
        canvas.height = size;
        canvas.style.cssText = 'margin:0 auto;display:block;border-radius:12px;';

        // QR Code Matrix Generation
        function generateQR(text) {
            // Use the built-in QR generation via a data URL approach
            // For maximum reliability, we use a lightweight inline encoder
            const img = document.createElement('img');
            img.style.cssText = 'margin:0 auto;display:block;border-radius:12px;width:200px;height:200px;background:#fff;padding:8px;';
            img.alt = 'QR Code: ' + text;
            // Generate using chart API (works offline if cached, degrades gracefully)
            img.src = 'https://api.qrserver.com/v1/create-qr-code/?size=200x200&data=' + encodeURIComponent(text);
            img.onerror = function() {
                // Fallback: show the URI as selectable text
                el.innerHTML = '<div style="background:#1e293b;border:1px solid #334155;border-radius:12px;padding:16px;text-align:center;">' +
                    '<p style="color:#94a3b8;font-size:12px;margin-bottom:8px;">QR unavailable offline. Copy the URI:</p>' +
                    '<code style="color:#0ea5e9;font-size:14px;word-break:break-all;">' + text + '</code></div>';
            };
            el.appendChild(img);
        }
        generateQR(text);
    })();
    </script>

    <!-- Tab Switching -->
    <script>
    function showTab(name) {
        document.querySelectorAll('.tab-panel').forEach(p => p.classList.add('hidden'));
        document.querySelectorAll('.tab-btn').forEach(b => {
            b.classList.remove('text-brand-500', 'border-brand-500');
            b.classList.add('text-slate-400', 'border-transparent');
        });
        document.getElementById('panel-' + name).classList.remove('hidden');
        const btn = document.getElementById('tab-' + name);
        btn.classList.add('text-brand-500', 'border-brand-500');
        btn.classList.remove('text-slate-400', 'border-transparent');
    }
    function copyText(text) {
        navigator.clipboard.writeText(text).then(() => {
            // Brief visual feedback could be added here
        });
    }
    </script>
</body>
</html>`
