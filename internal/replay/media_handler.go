package replay

import (
	"errors"
	"net/http"
	"strings"
)

type SegmentProvider interface {
	RouteSegments(dongleID, route string) ([]Segment, error)
}

type ReplaySegmentProvider interface {
	ReplaySegments(dongleID, route string) ([]Segment, error)
}

type ScopedReplaySegmentProvider interface {
	ReplaySegment(dongleID, route string, segment int) (Segment, error)
}

type MediaPlaylistProvider interface {
	MediaSegments(dongleID, route string) ([]Segment, error)
}

type ScopedMediaPlaylistProvider interface {
	MediaSegment(dongleID, route string, segment int) (Segment, error)
}

type MediaHandler struct {
	tickets  *TicketManager
	provider SegmentProvider
	locator  *Locator

	afterMediaOpen func()
}

func NewMediaHandler(tickets *TicketManager, provider SegmentProvider, locator *Locator) *MediaHandler {
	return &MediaHandler{tickets: tickets, provider: provider, locator: locator}
}

func (h *MediaHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /media/hls/{ticket}/index.m3u8", h.servePlaylist)
	mux.HandleFunc("GET /media/hls/{ticket}/{file}", h.serveSegment)
}

func (h *MediaHandler) servePlaylist(w http.ResponseWriter, r *http.Request) {
	ticket, ok := h.verifyTicket(w, r)
	if !ok {
		return
	}
	segments, ok := h.loadReplaySegments(w, ticket)
	if !ok {
		return
	}
	playlist, err := BuildPlaylist(ticket, segments)
	if errors.Is(err, ErrNoPlayableSegments) {
		http.Error(w, "media not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "failed to build playlist", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write([]byte(playlist))
}

func (h *MediaHandler) serveSegment(w http.ResponseWriter, r *http.Request) {
	ticket, ok := h.verifyTicket(w, r)
	if !ok {
		return
	}
	filename := r.PathValue("file")
	numberText, ok := strings.CutSuffix(filename, ".ts")
	if !ok {
		http.NotFound(w, r)
		return
	}
	number, err := parseSegmentNumber(numberText)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ticket.Mode == TicketModeSegment && number != *ticket.Segment {
		http.Error(w, "segment is outside ticket scope", http.StatusForbidden)
		return
	}
	segments, ok := h.loadSegments(w, ticket)
	if !ok {
		return
	}

	var selected *Segment
	for i := range segments {
		if segments[i].Number == number && segments[i].QCameraRelPath != "" {
			selected = &segments[i]
			break
		}
	}
	if selected == nil {
		http.NotFound(w, r)
		return
	}
	file, err := h.locator.OpenQCamera(ticket.DongleID, ticket.Route, *selected)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	if h.afterMediaOpen != nil {
		h.afterMediaOpen()
	}
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "failed to inspect media", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, filename, info.ModTime(), file.File)
}

func (h *MediaHandler) verifyTicket(w http.ResponseWriter, r *http.Request) (MediaTicket, bool) {
	ticket, err := h.tickets.Verify(r.PathValue("ticket"))
	if err != nil {
		http.Error(w, "invalid media ticket", http.StatusUnauthorized)
		return MediaTicket{}, false
	}
	return ticket, true
}

func (h *MediaHandler) loadSegments(w http.ResponseWriter, ticket MediaTicket) ([]Segment, bool) {
	segments, err := h.provider.RouteSegments(ticket.DongleID, ticket.Route)
	if err != nil {
		writeMediaProviderError(w, err)
		return nil, false
	}
	return segments, true
}

func (h *MediaHandler) loadReplaySegments(w http.ResponseWriter, ticket MediaTicket) ([]Segment, bool) {
	if ticket.Mode == TicketModeSegment && ticket.Segment != nil {
		if provider, ok := h.provider.(ScopedMediaPlaylistProvider); ok {
			segment, err := provider.MediaSegment(ticket.DongleID, ticket.Route, *ticket.Segment)
			if err != nil {
				writeMediaProviderError(w, err)
				return nil, false
			}
			return []Segment{segment}, true
		}
		if provider, ok := h.provider.(ScopedReplaySegmentProvider); ok {
			segment, err := provider.ReplaySegment(ticket.DongleID, ticket.Route, *ticket.Segment)
			if err != nil {
				if errors.Is(err, ErrMediaNotFound) || errors.Is(err, ErrRouteNotFound) {
					http.Error(w, "media not found", http.StatusNotFound)
				} else {
					http.Error(w, "failed to load route", http.StatusInternalServerError)
				}
				return nil, false
			}
			return []Segment{segment}, true
		}
	}
	if provider, ok := h.provider.(MediaPlaylistProvider); ok {
		segments, err := provider.MediaSegments(ticket.DongleID, ticket.Route)
		if err != nil {
			writeMediaProviderError(w, err)
			return nil, false
		}
		return segments, true
	}
	provider, ok := h.provider.(ReplaySegmentProvider)
	if !ok {
		return h.loadSegments(w, ticket)
	}
	segments, err := provider.ReplaySegments(ticket.DongleID, ticket.Route)
	if err != nil {
		writeMediaProviderError(w, err)
		return nil, false
	}
	return segments, true
}

func writeMediaProviderError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrMediaNotFound) || errors.Is(err, ErrRouteNotFound) {
		http.Error(w, "media not found", http.StatusNotFound)
		return
	}
	http.Error(w, "failed to load route", http.StatusInternalServerError)
}
