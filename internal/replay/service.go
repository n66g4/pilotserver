package replay

import (
	"errors"
	"fmt"
	"io/fs"
	"time"

	"pilotserver/internal/store"
)

var (
	ErrRouteNotFound        = errors.New("route not found")
	ErrMediaNotFound        = errors.New("media not found")
	ErrInvalidReplayRequest = errors.New("invalid replay request")
	ErrTelemetryNotFound    = errors.New("telemetry not found")
)

type SegmentStore interface {
	ListSegments(dongleID, route string) ([]store.Segment, error)
}

type SegmentSummary struct {
	Number            int     `json:"number"`
	Duration          float64 `json:"duration"`
	DurationEstimated bool    `json:"duration_estimated"`
	HasVideo          bool    `json:"has_video"`
	HasTelemetry      bool    `json:"has_telemetry"`
	TelemetryError    string  `json:"telemetry_error"`
}

type ReplaySummary struct {
	Route    string           `json:"route"`
	Duration float64          `json:"duration"`
	Segments []SegmentSummary `json:"segments"`
}

type Service struct {
	store   SegmentStore
	tickets *TicketManager
	locator *Locator
	cache   *Cache
}

func NewService(st SegmentStore, tickets *TicketManager) *Service {
	return &Service{store: st, tickets: tickets}
}

func NewServiceWithTelemetry(st SegmentStore, tickets *TicketManager, locator *Locator, cache *Cache) *Service {
	return &Service{store: st, tickets: tickets, locator: locator, cache: cache}
}

func (s *Service) RouteSegments(dongleID, route string) ([]Segment, error) {
	files, err := s.store.ListSegments(dongleID, route)
	if err != nil {
		return nil, fmt.Errorf("list route segments: %w", err)
	}
	if len(files) == 0 {
		return nil, ErrRouteNotFound
	}
	segments, err := BuildSegments(route, files)
	if err != nil {
		return nil, fmt.Errorf("build route segments: %w", err)
	}
	return segments, nil
}

func (s *Service) Summary(dongleID, route string) (ReplaySummary, error) {
	segments, err := s.ReplaySegments(dongleID, route)
	if err != nil {
		return ReplaySummary{}, err
	}
	summary := ReplaySummary{
		Route:    route,
		Segments: make([]SegmentSummary, 0, len(segments)),
	}
	for _, segment := range segments {
		summary.Duration += segment.Duration
		summary.Segments = append(summary.Segments, SegmentSummary{
			Number:            segment.Number,
			Duration:          segment.Duration,
			DurationEstimated: segment.DurationEstimated,
			HasVideo:          segment.QCameraRelPath != "",
			HasTelemetry:      segment.QlogRelPath != "",
			TelemetryError:    segment.TelemetryError,
		})
	}
	return summary, nil
}

func (s *Service) Telemetry(dongleID, route string, segment int) (SegmentTelemetry, error) {
	if segment < 0 {
		return SegmentTelemetry{}, fmt.Errorf("%w: %d", ErrInvalidSegment, segment)
	}
	if s.locator == nil || s.cache == nil {
		return SegmentTelemetry{}, errors.New("telemetry service is not configured")
	}
	segments, err := s.RouteSegments(dongleID, route)
	if errors.Is(err, ErrRouteNotFound) {
		return SegmentTelemetry{}, ErrTelemetryNotFound
	}
	if err != nil {
		return SegmentTelemetry{}, err
	}
	for _, candidate := range segments {
		if candidate.Number == segment {
			return s.loadTelemetry(dongleID, route, candidate)
		}
	}
	return SegmentTelemetry{}, ErrTelemetryNotFound
}

func (s *Service) ReplaySegments(dongleID, route string) ([]Segment, error) {
	segments, err := s.RouteSegments(dongleID, route)
	if err != nil {
		return nil, err
	}
	if s.locator == nil {
		return segments, nil
	}
	for i := range segments {
		s.enrichSegment(dongleID, route, &segments[i])
	}
	return segments, nil
}

func (s *Service) ReplaySegment(dongleID, route string, number int) (Segment, error) {
	if number < 0 {
		return Segment{}, fmt.Errorf("%w: %d", ErrInvalidSegment, number)
	}
	segments, err := s.RouteSegments(dongleID, route)
	if err != nil {
		return Segment{}, err
	}
	for i := range segments {
		if segments[i].Number == number {
			if s.locator != nil {
				s.enrichSegment(dongleID, route, &segments[i])
			}
			return segments[i], nil
		}
	}
	return Segment{}, ErrMediaNotFound
}

func (s *Service) MediaSegments(dongleID, route string) ([]Segment, error) {
	segments, err := s.RouteSegments(dongleID, route)
	if err != nil {
		return nil, err
	}
	if s.locator == nil {
		return segments, nil
	}
	for i := range segments {
		s.enrichMediaSegment(dongleID, route, &segments[i])
	}
	return segments, nil
}

func (s *Service) MediaSegment(dongleID, route string, number int) (Segment, error) {
	if number < 0 {
		return Segment{}, fmt.Errorf("%w: %d", ErrInvalidSegment, number)
	}
	segments, err := s.RouteSegments(dongleID, route)
	if err != nil {
		return Segment{}, err
	}
	for i := range segments {
		if segments[i].Number == number {
			if s.locator != nil {
				s.enrichMediaSegment(dongleID, route, &segments[i])
			}
			return segments[i], nil
		}
	}
	return Segment{}, ErrMediaNotFound
}

func (s *Service) enrichSegment(dongleID, route string, segment *Segment) {
	if segment.QCameraRelPath != "" {
		file, err := s.locator.OpenQCamera(dongleID, route, *segment)
		if err != nil {
			segment.QCameraRelPath = ""
		} else {
			file.Close()
		}
	}
	if segment.QlogRelPath == "" || s.cache == nil {
		return
	}
	telemetry, err := s.loadTelemetry(dongleID, route, *segment)
	if err != nil {
		segment.TelemetryError = telemetryErrorCode(err)
		return
	}
	segment.Duration = telemetry.Duration
	segment.DurationEstimated = telemetry.DurationEstimated
	segment.TelemetryError = ""
}

func (s *Service) enrichMediaSegment(dongleID, route string, segment *Segment) {
	if segment.QCameraRelPath != "" {
		file, err := s.locator.OpenQCamera(dongleID, route, *segment)
		if err != nil {
			segment.QCameraRelPath = ""
		} else {
			file.Close()
		}
	}
	if segment.QlogRelPath == "" || s.cache == nil {
		return
	}
	source, err := s.locator.OpenQlog(dongleID, route, *segment)
	if err != nil {
		return
	}
	telemetry, hit, err := s.cache.LoadCachedFile(
		dongleID, route, segment.Number, source,
	)
	if err != nil || !hit {
		return
	}
	segment.Duration = telemetry.Duration
	segment.DurationEstimated = telemetry.DurationEstimated
}

func (s *Service) loadTelemetry(dongleID, route string, segment Segment) (SegmentTelemetry, error) {
	if segment.QlogRelPath == "" {
		return SegmentTelemetry{}, ErrTelemetryNotFound
	}
	source, err := s.locator.OpenQlog(dongleID, route, segment)
	if errors.Is(err, fs.ErrNotExist) {
		return SegmentTelemetry{}, ErrTelemetryNotFound
	}
	if err != nil {
		return SegmentTelemetry{}, fmt.Errorf("locate telemetry: %w", err)
	}
	return s.cache.LoadFile(dongleID, route, segment.Number, source)
}

func telemetryErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrZstd):
		return "invalid_zstd"
	case errors.Is(err, ErrCapnp):
		return "invalid_capnp"
	case errors.Is(err, ErrDecompressedLimit):
		return "decompressed_limit"
	case errors.Is(err, ErrEventLimit):
		return "event_limit"
	case errors.Is(err, ErrTelemetrySourceChanged):
		return "source_changed"
	default:
		return "unavailable"
	}
}

func (s *Service) IssueMediaTicket(dongleID, route string, mode TicketMode, segment *int) (string, time.Time, error) {
	if !validTicketScope(mode, segment) {
		return "", time.Time{}, ErrInvalidReplayRequest
	}
	segments, err := s.RouteSegments(dongleID, route)
	if err != nil {
		return "", time.Time{}, err
	}
	if s.locator != nil {
		for i := range segments {
			if segments[i].QCameraRelPath == "" {
				continue
			}
			file, openErr := s.locator.OpenQCamera(dongleID, route, segments[i])
			if openErr != nil {
				segments[i].QCameraRelPath = ""
				continue
			}
			file.Close()
		}
	}
	if !hasRequestedVideo(segments, mode, segment) {
		return "", time.Time{}, ErrMediaNotFound
	}
	token, expiresAt, err := s.tickets.Issue(dongleID, route, mode, segment)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("issue media ticket: %w", err)
	}
	return token, expiresAt, nil
}

func validTicketScope(mode TicketMode, segment *int) bool {
	switch mode {
	case TicketModeRoute:
		return segment == nil
	case TicketModeSegment:
		return segment != nil && *segment >= 0
	default:
		return false
	}
}

func hasRequestedVideo(segments []Segment, mode TicketMode, requested *int) bool {
	for _, segment := range segments {
		if segment.QCameraRelPath != "" &&
			(mode == TicketModeRoute || segment.Number == *requested) {
			return true
		}
	}
	return false
}
