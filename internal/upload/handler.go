package upload

import (
	"errors"
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

const MaxUploadSize int64 = 512 << 20

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
	if len(strings.Split(relPath, "/")) < 2 {
		return fmt.Errorf("upload path must contain segment directory and filename")
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
	if r.ContentLength > MaxUploadSize {
		http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
		return
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".upload-*")
	if err != nil {
		http.Error(w, "create upload file", http.StatusInternalServerError)
		return
	}
	tempName := file.Name()
	defer os.Remove(tempName)
	size, copyErr := io.Copy(file, http.MaxBytesReader(w, r.Body, MaxUploadSize))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(copyErr, &maxBytesErr) {
			http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "write upload file", http.StatusInternalServerError)
		return
	}

	if err := os.Rename(tempName, target); err != nil {
		http.Error(w, "commit upload file", http.StatusInternalServerError)
		return
	}
	parts := strings.Split(claim.RelPath, "/")
	segmentName := parts[0]
	if len(parts) > 2 {
		segmentName = parts[1]
	}
	if err := h.store.InsertSegment(store.Segment{
		DongleID:    claim.DongleID,
		RouteName:   parts[0],
		SegmentName: segmentName,
		RelPath:     claim.RelPath,
		Size:        size,
		UploadedAt:  time.Now().Unix(),
	}); err != nil {
		http.Error(w, "record upload", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func validDongleID(dongleID string) bool {
	return dongleID != "" && !strings.ContainsAny(dongleID, `/\`) &&
		dongleID != "." && dongleID != ".."
}
