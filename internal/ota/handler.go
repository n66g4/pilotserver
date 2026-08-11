package ota

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"pilotserver/internal/config"
)

type Handler struct {
	dataDir    string
	fileServer http.Handler
}

func Mount(mux *http.ServeMux, cfg config.Config) {
	_ = EnsureDirs(cfg.DataDir)
	filesDir := filepath.Join(cfg.DataDir, "ota", "files")
	h := &Handler{
		dataDir:    cfg.DataDir,
		fileServer: http.FileServer(http.Dir(filesDir)),
	}
	mux.HandleFunc("GET /ota/{path...}", h.serve)
}

func EnsureDirs(dataDir string) error {
	return os.MkdirAll(filepath.Join(dataDir, "ota", "files"), 0o755)
}

func validChannel(channel string) bool {
	return channel != "" && channel != "files" && !strings.ContainsAny(channel, `/\`) &&
		channel != "." && channel != ".."
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	rest := r.PathValue("path")
	if rest == "files" || strings.HasPrefix(rest, "files/") {
		h.serveFiles(w, r, strings.TrimPrefix(rest, "files/"))
		return
	}
	channel, suffix, ok := strings.Cut(rest, "/")
	if ok && suffix == "version" && validChannel(channel) {
		h.serveVersion(w, r, channel)
		return
	}
	http.NotFound(w, r)
}

func (h *Handler) serveFiles(w http.ResponseWriter, r *http.Request, name string) {
	if strings.Contains(name, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	req := r.Clone(r.Context())
	if name == "" {
		req.URL.Path = "/"
	} else {
		req.URL.Path = "/" + name
	}
	h.fileServer.ServeHTTP(w, req)
}

func (h *Handler) serveVersion(w http.ResponseWriter, r *http.Request, channel string) {
	path := filepath.Join(h.dataDir, "ota", channel, "version.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "version not found", http.StatusNotFound)
			return
		}
		http.Error(w, "read version", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}
