package adminapi

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"pilotserver/internal/replay"
)

func handleReplay(w http.ResponseWriter, r *http.Request, service *replay.Service) {
	summary, err := service.Summary(r.PathValue("dongleID"), r.PathValue("route"))
	if err != nil {
		writeReplayError(w, err)
		return
	}
	writeJSON(w, summary)
}

func handleMediaTicket(w http.ResponseWriter, r *http.Request, service *replay.Service) {
	var request struct {
		Mode    replay.TicketMode `json:"mode"`
		Segment *int              `json:"segment"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	token, expiresAt, err := service.IssueMediaTicket(
		r.PathValue("dongleID"),
		r.PathValue("route"),
		request.Mode,
		request.Segment,
	)
	if err != nil {
		writeReplayError(w, err)
		return
	}
	writeJSON(w, struct {
		PlaylistURL string    `json:"playlist_url"`
		ExpiresAt   time.Time `json:"expires_at"`
	}{
		PlaylistURL: "/media/hls/" + url.PathEscape(token) + "/index.m3u8",
		ExpiresAt:   expiresAt,
	})
}

func handleTelemetry(w http.ResponseWriter, r *http.Request, service *replay.Service) {
	segment, err := parseTelemetrySegment(r.PathValue("segment"))
	if err != nil {
		http.Error(w, "invalid segment", http.StatusBadRequest)
		return
	}
	telemetry, err := service.Telemetry(
		r.PathValue("dongleID"), r.PathValue("route"), segment,
	)
	if err != nil {
		writeTelemetryError(w, err)
		return
	}
	writeJSON(w, telemetry)
}

func parseTelemetrySegment(value string) (int, error) {
	if value == "" {
		return 0, errors.New("empty segment")
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return 0, errors.New("invalid segment")
		}
	}
	segment, err := strconv.ParseUint(value, 10, 64)
	if err != nil || segment > uint64(math.MaxInt) {
		if err == nil {
			err = errors.New("segment overflows int")
		}
		return 0, err
	}
	return int(segment), nil
}

func writeTelemetryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, replay.ErrInvalidSegment):
		http.Error(w, "invalid segment", http.StatusBadRequest)
	case errors.Is(err, replay.ErrTelemetryNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, replay.ErrZstd),
		errors.Is(err, replay.ErrCapnp),
		errors.Is(err, replay.ErrDecompressedLimit),
		errors.Is(err, replay.ErrEventLimit),
		errors.Is(err, replay.ErrInvalidLimits):
		http.Error(w, "telemetry unavailable", http.StatusUnprocessableEntity)
	case errors.Is(err, replay.ErrTelemetrySourceChanged):
		http.Error(w, "telemetry source changed", http.StatusConflict)
	default:
		http.Error(w, "telemetry failed", http.StatusInternalServerError)
	}
}

func writeReplayError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, replay.ErrInvalidReplayRequest):
		http.Error(w, "invalid request", http.StatusBadRequest)
	case errors.Is(err, replay.ErrRouteNotFound), errors.Is(err, replay.ErrMediaNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		http.Error(w, "replay failed", http.StatusInternalServerError)
	}
}
