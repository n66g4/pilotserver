package adminapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"pilotserver/internal/store"
	"pilotserver/internal/upload"
)

func handleRoutes(w http.ResponseWriter, r *http.Request, st *store.Store) {
	routes, err := st.ListRoutes(r.PathValue("dongleID"))
	if err != nil {
		http.Error(w, "list routes", http.StatusInternalServerError)
		return
	}
	writeJSON(w, routes)
}

func handleSegments(w http.ResponseWriter, r *http.Request, st *store.Store) {
	segments, err := st.ListSegments(r.PathValue("dongleID"), r.PathValue("route"))
	if err != nil {
		http.Error(w, "list segments", http.StatusInternalServerError)
		return
	}
	writeJSON(w, segments)
}

func handleDownload(w http.ResponseWriter, r *http.Request, dataDir string) {
	relPath := r.PathValue("route") + "/" + r.PathValue("path")
	if err := upload.ValidateRelPath(relPath); err != nil {
		http.Error(w, "invalid upload path", http.StatusBadRequest)
		return
	}
	fileName := filepath.Join(dataDir, "uploads", r.PathValue("dongleID"), filepath.FromSlash(relPath))
	info, err := os.Stat(fileName)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(fileName)+`"`)
	http.ServeFile(w, r, fileName)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
