package admin

import (
	_ "embed"
	"net/http"

	"pilotserver/internal/replay/cereal"
)

//go:embed index.html
var indexHTML []byte

//go:embed vendor/hls.min.js
var hlsJS []byte

//go:embed vendor/hls.js.LICENSE.txt
var hlsLicense []byte

//go:embed vendor/xterm.min.js
var xtermJS []byte

//go:embed vendor/xterm.css
var xtermCSS []byte

//go:embed vendor/xterm.LICENSE.txt
var xtermLicense []byte

//go:embed telemetry.js
var telemetryJS []byte

//go:embed i18n.js
var i18nJS []byte

func ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body []byte
	switch r.URL.Path {
	case "/admin/", "/admin/index.html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		body = indexHTML
	case "/admin/vendor/hls.min.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		body = hlsJS
	case "/admin/vendor/hls.js.LICENSE.txt":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		body = hlsLicense
	case "/admin/vendor/xterm.min.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		body = xtermJS
	case "/admin/vendor/xterm.css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		body = xtermCSS
	case "/admin/vendor/xterm.LICENSE.txt":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		body = xtermLicense
	case "/admin/licenses/dragonpilot.txt":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		body = cereal.DragonpilotLicense
	case "/admin/licenses/openpilot.txt":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		body = cereal.OpenpilotLicense
	case "/admin/telemetry.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		body = telemetryJS
	case "/admin/i18n.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		body = i18nJS
	default:
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(body)
}
