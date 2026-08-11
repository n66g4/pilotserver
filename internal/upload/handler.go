package upload

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"pilotserver/internal/config"
	"pilotserver/internal/store"
)

type Handler struct {
	store     *store.Store
	dataDir   string
	jwtSecret string
}

func Mount(mux *http.ServeMux, st *store.Store, cfg config.Config) {
	handler := &Handler{store: st, dataDir: cfg.DataDir, jwtSecret: cfg.JWTSecret}
	mux.HandleFunc("PUT /upload/put/{token}", handler.put)
}

func ValidateRelPath(relPath string) error {
	if relPath == "" || strings.Contains(relPath, `\`) || path.IsAbs(relPath) ||
		path.Clean(relPath) != relPath || strings.HasPrefix(relPath, "../") {
		return fmt.Errorf("invalid upload path")
	}
	if len(strings.Split(relPath, "/")) < 3 {
		return fmt.Errorf("upload path must contain route, segment, and filename")
	}
	return nil
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	claim, err := Verify(h.jwtSecret, r.PathValue("token"))
	if err != nil {
		http.Error(w, "invalid upload token", http.StatusUnauthorized)
		return
	}
	if err := ValidateRelPath(claim.RelPath); err != nil || !validDongleID(claim.DongleID) {
		http.Error(w, "invalid upload path", http.StatusBadRequest)
		return
	}

	target := filepath.Join(h.dataDir, "uploads", claim.DongleID, filepath.FromSlash(claim.RelPath))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		http.Error(w, "create upload directory", http.StatusInternalServerError)
		return
	}
	file, err := os.Create(target)
	if err != nil {
		http.Error(w, "create upload file", http.StatusInternalServerError)
		return
	}
	size, copyErr := io.Copy(file, r.Body)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(target)
		http.Error(w, "write upload file", http.StatusInternalServerError)
		return
	}

	parts := strings.Split(claim.RelPath, "/")
	if err := h.store.InsertSegment(store.Segment{
		DongleID:    claim.DongleID,
		RouteName:   parts[0],
		SegmentName: parts[1],
		RelPath:     claim.RelPath,
		Size:        size,
		UploadedAt:  time.Now().Unix(),
	}); err != nil {
		_ = os.Remove(target)
		http.Error(w, "record upload", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validDongleID(dongleID string) bool {
	return dongleID != "" && !strings.ContainsAny(dongleID, `/\`) &&
		dongleID != "." && dongleID != ".."
}
