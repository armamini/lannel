package web

import (
	"fmt"
	"strings"
)

// generateQRSVG generates a QR code as an inline SVG string.
// Uses a simple API-free approach: renders a QR code via an embedded
// JavaScript library in the HTML template. This function produces
// a placeholder div with a data attribute that the frontend JS picks up.
// For a fully offline solution, we generate a "manual" QR representation.
//
// Since we want zero external dependencies and no network calls,
// we use a JS-based QR generator embedded directly in the HTML.
func qrContainerHTML(content string) string {
	return fmt.Sprintf(`<div id="qrcode" data-content="%s"></div>`, content)
}

// generateQRInfoBlock returns the full QR section HTML with the
// SOCKS5 URI and a JS-powered QR code renderer.
func generateQRInfoBlock(lanIP string, socksPort int) string {
	socksURI := fmt.Sprintf("socks5://%s:%d", lanIP, socksPort)

	var b strings.Builder
	b.WriteString(`<div class="text-center">`)
	b.WriteString(fmt.Sprintf(`<p class="text-sm text-slate-400 mb-3 font-mono">%s</p>`, socksURI))
	b.WriteString(qrContainerHTML(socksURI))
	b.WriteString(`<p class="text-xs text-slate-500 mt-3">Scan with v2rayNG / Shadowrocket / any SOCKS5 client</p>`)
	b.WriteString(`</div>`)
	return b.String()
}
