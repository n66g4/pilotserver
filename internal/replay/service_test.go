package replay

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"pilotserver/internal/store"
)

type serviceSegmentStore struct {
	segments []store.Segment
	err      error
}

func (s serviceSegmentStore) ListSegments(_, _ string) ([]store.Segment, error) {
	return s.segments, s.err
}

func TestServiceSummary(t *testing.T) {
	service := NewService(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "route", SegmentName: "10", RelPath: "route/10/qlog.zst"},
		{RouteName: "route", SegmentName: "2", RelPath: "route/2/qcamera.ts"},
		{RouteName: "route", SegmentName: "2", RelPath: "route/2/qlog.zst"},
	}}, NewTicketManager("test-secret", time.Minute))

	summary, err := service.Summary("dongle", "route")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Route != "route" || summary.Duration != 540 {
		t.Fatalf("summary = %+v", summary)
	}
	if len(summary.Segments) != 9 {
		t.Fatalf("segments = %+v", summary.Segments)
	}
	if got := summary.Segments[0]; got.Number != 2 || got.Duration != 60 ||
		!got.DurationEstimated || !got.HasVideo || !got.HasTelemetry || got.TelemetryError != "" {
		t.Fatalf("segment 0 = %+v", got)
	}
	for _, got := range summary.Segments[1:8] {
		if got.HasVideo || got.HasTelemetry {
			t.Fatalf("gap segment = %+v", got)
		}
	}
	if got := summary.Segments[8]; got.Number != 10 || got.HasVideo || !got.HasTelemetry {
		t.Fatalf("segment 8 = %+v", got)
	}
}

func TestServiceRouteNotFound(t *testing.T) {
	service := NewService(serviceSegmentStore{}, NewTicketManager("test-secret", time.Minute))

	_, err := service.Summary("dongle", "missing")
	if !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("error = %v, want ErrRouteNotFound", err)
	}
}

func TestServiceSummaryAllowsOnlyUnrecognizedFiles(t *testing.T) {
	service := NewService(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/rlog.bz2"},
	}}, NewTicketManager("test-secret", time.Minute))

	summary, err := service.Summary("dongle", "route")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Route != "route" || summary.Duration != 0 || len(summary.Segments) != 0 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestServiceSummarySupportsDragonPilotTwoLevelPaths(t *testing.T) {
	service := NewService(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "00000010--2cbbf69c9f", SegmentName: "0", RelPath: "00000010--2cbbf69c9f--0/qcamera.ts"},
		{RouteName: "00000010--2cbbf69c9f", SegmentName: "0", RelPath: "00000010--2cbbf69c9f--0/qlog.zst"},
	}}, NewTicketManager("test-secret", time.Minute))

	summary, err := service.Summary("dongle", "00000010--2cbbf69c9f")
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Segments) != 1 ||
		!summary.Segments[0].HasVideo ||
		!summary.Segments[0].HasTelemetry {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestServiceSummaryIgnoresBootFiles(t *testing.T) {
	service := NewService(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "boot", SegmentName: "boot", RelPath: "boot/qlog.zst"},
		{RouteName: "boot", SegmentName: "boot", RelPath: "boot/bootlog"},
		{RouteName: "boot", SegmentName: "boot", RelPath: "boot/qlog.zst.uploading"},
	}}, NewTicketManager("test-secret", time.Minute))

	summary, err := service.Summary("dongle", "boot")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Route != "boot" || summary.Duration != 0 || len(summary.Segments) != 0 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestServiceRouteTicketRequiresVideo(t *testing.T) {
	service := NewService(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qlog.zst"},
	}}, NewTicketManager("test-secret", time.Minute))

	_, _, err := service.IssueMediaTicket("dongle", "route", TicketModeRoute, nil)
	if !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("error = %v, want ErrMediaNotFound", err)
	}
}

func TestServiceSegmentTicket(t *testing.T) {
	segment := 2
	tickets := NewTicketManager("test-secret", time.Minute)
	service := NewService(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "route", SegmentName: "2", RelPath: "route/2/qcamera.ts"},
	}}, tickets)

	token, expiresAt, err := service.IssueMediaTicket("dongle", "route", TicketModeSegment, &segment)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := tickets.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.DongleID != "dongle" || ticket.Route != "route" ||
		ticket.Mode != TicketModeSegment || ticket.Segment == nil || *ticket.Segment != segment {
		t.Fatalf("ticket = %+v", ticket)
	}
	if !ticket.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("ticket expiry = %v, response expiry = %v", ticket.ExpiresAt, expiresAt)
	}
}

func TestServiceSegmentTicketRejectsUnavailableVideo(t *testing.T) {
	tests := []struct {
		name     string
		segments []store.Segment
		segment  int
	}{
		{
			name: "missing segment",
			segments: []store.Segment{
				{RouteName: "route", SegmentName: "2", RelPath: "route/2/qcamera.ts"},
			},
			segment: 3,
		},
		{
			name: "segment without video",
			segments: []store.Segment{
				{RouteName: "route", SegmentName: "2", RelPath: "route/2/qlog.zst"},
			},
			segment: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(serviceSegmentStore{segments: tt.segments}, NewTicketManager("test-secret", time.Minute))

			_, _, err := service.IssueMediaTicket("dongle", "route", TicketModeSegment, &tt.segment)
			if !errors.Is(err, ErrMediaNotFound) {
				t.Fatalf("error = %v, want ErrMediaNotFound", err)
			}
		})
	}
}

func TestServiceRejectsInvalidTicketScope(t *testing.T) {
	segment := 0
	tests := []struct {
		name    string
		mode    TicketMode
		segment *int
	}{
		{name: "unknown mode", mode: TicketMode("unknown")},
		{name: "route with segment", mode: TicketModeRoute, segment: &segment},
		{name: "segment without number", mode: TicketModeSegment},
	}
	service := NewService(serviceSegmentStore{}, NewTicketManager("test-secret", time.Minute))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := service.IssueMediaTicket("dongle", "route", tt.mode, tt.segment)
			if !errors.Is(err, ErrInvalidReplayRequest) {
				t.Fatalf("error = %v, want ErrInvalidReplayRequest", err)
			}
		})
	}
}

func TestServicePreservesStoreError(t *testing.T) {
	storeErr := errors.New("store failed")
	service := NewService(serviceSegmentStore{err: storeErr}, NewTicketManager("test-secret", time.Minute))

	_, err := service.RouteSegments("dongle", "route")
	if !errors.Is(err, storeErr) {
		t.Fatalf("error = %v, want wrapped store error", err)
	}
}

func TestServiceTelemetryFindsExactSegment(t *testing.T) {
	dataDir := t.TempDir()
	writeServiceQlog(t, dataDir, "dongle", "route", 2)
	parser := &cacheParser{result: testTelemetry(2)}
	service := NewServiceWithTelemetry(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "route", SegmentName: "1", RelPath: "route/1/qlog.zst"},
		{RouteName: "route", SegmentName: "2", RelPath: "route/2/qlog.zst"},
	}}, NewTicketManager("test-secret", time.Minute), NewLocator(dataDir), NewCache(dataDir, parser))

	telemetry, err := service.Telemetry("dongle", "route", 2)
	if err != nil {
		t.Fatal(err)
	}
	if telemetry.Segment != 2 || parser.callCount() != 1 {
		t.Fatalf("telemetry/calls = %+v/%d", telemetry, parser.callCount())
	}
}

func TestServiceTelemetryMapsMissingRouteSegmentAndQlog(t *testing.T) {
	dataDir := t.TempDir()
	service := NewServiceWithTelemetry(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "route", SegmentName: "2", RelPath: "route/2/qcamera.ts"},
	}}, NewTicketManager("test-secret", time.Minute), NewLocator(dataDir),
		NewCache(dataDir, &cacheParser{result: testTelemetry(2)}))

	for _, test := range []struct {
		name    string
		store   SegmentStore
		segment int
	}{
		{name: "route", store: serviceSegmentStore{}, segment: 2},
		{name: "segment", store: service.store, segment: 3},
		{name: "qlog metadata", store: service.store, segment: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			service.store = test.store
			_, err := service.Telemetry("dongle", "route", test.segment)
			if !errors.Is(err, ErrTelemetryNotFound) {
				t.Fatalf("error = %v, want ErrTelemetryNotFound", err)
			}
		})
	}
	if _, err := service.Telemetry("dongle", "route", -1); !errors.Is(err, ErrInvalidSegment) {
		t.Fatalf("negative segment error = %v", err)
	}
	if _, err := NewService(serviceSegmentStore{}, nil).Telemetry("dongle", "route", 0); err == nil {
		t.Fatal("Telemetry without components succeeded")
	}
}

func TestServiceSummaryUsesMeasuredDurationsAndIsolatesSegmentFailures(t *testing.T) {
	dataDir := t.TempDir()
	writeServiceQlog(t, dataDir, "dongle", "route", 0)
	writeServiceQlog(t, dataDir, "dongle", "route", 1)
	parser := &segmentResultParser{
		results: map[int]SegmentTelemetry{0: {Segment: 0, Duration: 7.25}},
		errs:    map[int]error{1: ErrCapnp},
	}
	service := NewServiceWithTelemetry(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qlog.zst"},
		{RouteName: "route", SegmentName: "1", RelPath: "route/1/qlog.zst"},
	}}, NewTicketManager("test-secret", time.Minute), NewLocator(dataDir), NewCache(dataDir, parser))

	summary, err := service.Summary("dongle", "route")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Duration != 67.25 || len(summary.Segments) != 2 {
		t.Fatalf("summary = %+v", summary)
	}
	if got := summary.Segments[0]; got.Duration != 7.25 || got.DurationEstimated || got.TelemetryError != "" {
		t.Fatalf("measured segment = %+v", got)
	}
	if got := summary.Segments[1]; got.Duration != 60 || !got.DurationEstimated ||
		!got.HasTelemetry || got.TelemetryError != "invalid_capnp" {
		t.Fatalf("failed segment = %+v", got)
	}
}

func TestServiceReplaySegmentsUsesStableTelemetryErrorCodes(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{ErrZstd, "invalid_zstd"},
		{ErrCapnp, "invalid_capnp"},
		{ErrDecompressedLimit, "decompressed_limit"},
		{ErrEventLimit, "event_limit"},
		{ErrTelemetrySourceChanged, "source_changed"},
		{errors.New("/private/uploads/secret/qlog.zst"), "unavailable"},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			dataDir := t.TempDir()
			writeServiceQlog(t, dataDir, "dongle", "route", 0)
			service := NewServiceWithTelemetry(serviceSegmentStore{segments: []store.Segment{{
				RouteName: "route", SegmentName: "0", RelPath: "route/0/qlog.zst",
			}}}, nil, NewLocator(dataDir), NewCache(dataDir, &cacheParser{err: test.err}))
			segments, err := service.ReplaySegments("dongle", "route")
			if err != nil {
				t.Fatal(err)
			}
			if len(segments) != 1 || segments[0].TelemetryError != test.code {
				t.Fatalf("segments = %+v", segments)
			}
		})
	}
}

func TestServiceReplaySegmentParsesOnlyRequestedSegment(t *testing.T) {
	dataDir := t.TempDir()
	writeServiceQlog(t, dataDir, "dongle", "route", 0)
	writeServiceQlog(t, dataDir, "dongle", "route", 1)
	parser := &recordingSegmentParser{}
	service := NewServiceWithTelemetry(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qcamera.ts"},
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qlog.zst"},
		{RouteName: "route", SegmentName: "1", RelPath: "route/1/qcamera.ts"},
		{RouteName: "route", SegmentName: "1", RelPath: "route/1/qlog.zst"},
	}}, nil, NewLocator(dataDir), NewCache(dataDir, parser))

	segment, err := service.ReplaySegment("dongle", "route", 1)
	if err != nil {
		t.Fatal(err)
	}
	if segment.Number != 1 || segment.Duration != 11 {
		t.Fatalf("segment = %+v", segment)
	}
	if len(parser.segments) != 1 || parser.segments[0] != 1 {
		t.Fatalf("parsed segments = %v, want [1]", parser.segments)
	}
}

func TestServiceReplaySegmentsParsesAllTelemetrySegments(t *testing.T) {
	dataDir := t.TempDir()
	writeServiceQlog(t, dataDir, "dongle", "route", 0)
	writeServiceQlog(t, dataDir, "dongle", "route", 1)
	parser := &recordingSegmentParser{}
	service := NewServiceWithTelemetry(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qlog.zst"},
		{RouteName: "route", SegmentName: "1", RelPath: "route/1/qlog.zst"},
	}}, nil, NewLocator(dataDir), NewCache(dataDir, parser))

	if _, err := service.ReplaySegments("dongle", "route"); err != nil {
		t.Fatal(err)
	}
	if len(parser.segments) != 2 || parser.segments[0] != 0 || parser.segments[1] != 1 {
		t.Fatalf("parsed segments = %v, want [0 1]", parser.segments)
	}
}

func TestServiceTicketIssuanceDoesNotParseTelemetry(t *testing.T) {
	dataDir := t.TempDir()
	writeServiceQCamera(t, dataDir, "dongle", "route", 0)
	parser := &cacheParser{result: testTelemetry(0)}
	service := NewServiceWithTelemetry(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qcamera.ts"},
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qlog.zst"},
	}}, NewTicketManager("test-secret", time.Minute), NewLocator(dataDir), NewCache(dataDir, parser))

	if _, _, err := service.IssueMediaTicket("dongle", "route", TicketModeRoute, nil); err != nil {
		t.Fatal(err)
	}
	if parser.callCount() != 0 {
		t.Fatalf("parser calls = %d, want 0", parser.callCount())
	}
}

func TestServiceSummaryAndTicketRequireSafelyOpenVideo(t *testing.T) {
	for _, name := range []string{"missing", "symlink"} {
		t.Run(name, func(t *testing.T) {
			dataDir := t.TempDir()
			target := filepath.Join(dataDir, "uploads", "dongle", "route", "0", "qcamera.ts")
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if name == "symlink" {
				outside := filepath.Join(t.TempDir(), "qcamera.ts")
				if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			}
			segment := 0
			service := NewServiceWithTelemetry(serviceSegmentStore{segments: []store.Segment{{
				RouteName: "route", SegmentName: "0", RelPath: "route/0/qcamera.ts",
			}}}, NewTicketManager("test-secret", time.Minute), NewLocator(dataDir),
				NewCache(dataDir, &cacheParser{}))

			summary, err := service.Summary("dongle", "route")
			if err != nil {
				t.Fatal(err)
			}
			if len(summary.Segments) != 1 || summary.Segments[0].HasVideo {
				t.Fatalf("summary = %+v, want unavailable video", summary)
			}
			if _, _, err := service.IssueMediaTicket(
				"dongle", "route", TicketModeSegment, &segment,
			); !errors.Is(err, ErrMediaNotFound) {
				t.Fatalf("ticket error = %v, want ErrMediaNotFound", err)
			}
		})
	}
}

type segmentResultParser struct {
	results map[int]SegmentTelemetry
	errs    map[int]error
}

type recordingSegmentParser struct {
	segments []int
}

func (p *recordingSegmentParser) ParseSegment(_ io.Reader, segment int) (SegmentTelemetry, error) {
	p.segments = append(p.segments, segment)
	return SegmentTelemetry{Segment: segment, Duration: float64(10 + segment)}, nil
}

func (p *segmentResultParser) ParseSegment(_ io.Reader, segment int) (SegmentTelemetry, error) {
	return p.results[segment], p.errs[segment]
}

func writeServiceQlog(t *testing.T, dataDir, dongleID, route string, segment int) {
	t.Helper()
	dir := filepath.Join(dataDir, "uploads", dongleID, route, strconv.Itoa(segment))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "qlog.zst"), []byte("qlog"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeServiceQCamera(t *testing.T, dataDir, dongleID, route string, segment int) {
	t.Helper()
	dir := filepath.Join(dataDir, "uploads", dongleID, route, strconv.Itoa(segment))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "qcamera.ts"), []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
}
