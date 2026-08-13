package replay

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"pilotserver/internal/store"
)

type stubSegmentProvider struct {
	segments []Segment
	err      error
	calls    int
}

type enrichedSegmentProvider struct {
	plain         []Segment
	enriched      []Segment
	scoped        Segment
	plainCalls    int
	enrichedCalls int
	scopedCalls   int
}

type mutableMediaStore struct {
	segments []store.Segment
}

func (s *mutableMediaStore) ListSegments(_, _ string) ([]store.Segment, error) {
	return s.segments, nil
}

func (p *enrichedSegmentProvider) RouteSegments(_, _ string) ([]Segment, error) {
	p.plainCalls++
	return p.plain, nil
}

func (p *enrichedSegmentProvider) ReplaySegments(_, _ string) ([]Segment, error) {
	p.enrichedCalls++
	return p.enriched, nil
}

func (p *enrichedSegmentProvider) ReplaySegment(_, _ string, _ int) (Segment, error) {
	p.scopedCalls++
	return p.scoped, nil
}

func (p *stubSegmentProvider) RouteSegments(dongleID, route string) ([]Segment, error) {
	p.calls++
	return p.segments, p.err
}

func TestMediaHandlerPlaylistGETAndHEAD(t *testing.T) {
	manager := NewTicketManager("secret", time.Minute)
	token, _, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := &stubSegmentProvider{segments: []Segment{{
		Number: 0, Duration: 1, QCameraRelPath: "route/0/qcamera.ts",
	}}}
	handler := newTestMediaHandler(manager, provider, t.TempDir())

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			response := serveMediaRequest(handler, method, "/media/hls/"+token+"/index.m3u8", "")
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", response.Code)
			}
			if got := response.Header().Get("Content-Type"); got != "application/vnd.apple.mpegurl" {
				t.Fatalf("Content-Type = %q", got)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q", got)
			}
			if method == http.MethodGet && !strings.Contains(response.Body.String(), "#EXTM3U") {
				t.Fatalf("GET body = %q", response.Body.String())
			}
			if method == http.MethodHead && response.Body.Len() != 0 {
				t.Fatalf("HEAD body = %q, want empty", response.Body.String())
			}
		})
	}
}

func TestMediaHandlerTSGETHEADAndRange(t *testing.T) {
	dataDir := t.TempDir()
	writeQCamera(t, dataDir, "dongle", "route", []byte("abcdef"))
	manager := NewTicketManager("secret", time.Minute)
	token, _, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := &stubSegmentProvider{segments: []Segment{{
		Number: 0, Duration: 1, QCameraRelPath: "route/0/qcamera.ts",
	}}}
	handler := newTestMediaHandler(manager, provider, dataDir)

	get := serveMediaRequest(handler, http.MethodGet, "/media/hls/"+token+"/0.ts", "")
	if get.Code != http.StatusOK || get.Body.String() != "abcdef" {
		t.Fatalf("GET status/body = %d/%q", get.Code, get.Body.String())
	}
	if get.Header().Get("Content-Type") != "video/mp2t" ||
		get.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("GET headers = %#v", get.Header())
	}

	head := serveMediaRequest(handler, http.MethodHead, "/media/hls/"+token+"/0.ts", "")
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD status/body = %d/%q", head.Code, head.Body.String())
	}

	ranged := serveMediaRequest(handler, http.MethodGet, "/media/hls/"+token+"/0.ts", "bytes=1-3")
	if ranged.Code != http.StatusPartialContent || ranged.Body.String() != "bcd" {
		t.Fatalf("Range status/body = %d/%q", ranged.Code, ranged.Body.String())
	}
	if ranged.Header().Get("Content-Range") != "bytes 1-3/6" {
		t.Fatalf("Content-Range = %q", ranged.Header().Get("Content-Range"))
	}
}

func TestMediaHandlerServesTwoLevelDragonPilotRouteAndScopedTickets(t *testing.T) {
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "uploads", "dongle", "route--with--parts--12")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "qcamera.ts"), []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := NewTicketManager("secret", time.Minute)
	selected := 12
	routeToken, _, err := manager.Issue("dongle", "route--with--parts", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	scopedToken, _, err := manager.Issue("dongle", "route--with--parts", TicketModeSegment, &selected)
	if err != nil {
		t.Fatal(err)
	}
	provider := &stubSegmentProvider{segments: []Segment{{
		Number: 12, Duration: 1, QCameraRelPath: "route--with--parts--12/qcamera.ts",
	}}}
	handler := newTestMediaHandler(manager, provider, dataDir)

	for _, token := range []string{routeToken, scopedToken} {
		playlist := serveMediaRequest(handler, http.MethodGet,
			"/media/hls/"+token+"/index.m3u8", "")
		if playlist.Code != http.StatusOK || !strings.Contains(playlist.Body.String(), "\n12.ts\n") {
			t.Fatalf("playlist status/body = %d/%q", playlist.Code, playlist.Body.String())
		}
		head := serveMediaRequest(handler, http.MethodHead, "/media/hls/"+token+"/12.ts", "")
		if head.Code != http.StatusOK || head.Body.Len() != 0 {
			t.Fatalf("HEAD status/body = %d/%q", head.Code, head.Body.String())
		}
		ranged := serveMediaRequest(handler, http.MethodGet,
			"/media/hls/"+token+"/12.ts", "bytes=1-3")
		if ranged.Code != http.StatusPartialContent || ranged.Body.String() != "bcd" {
			t.Fatalf("Range status/body = %d/%q", ranged.Code, ranged.Body.String())
		}
	}
}

func TestMediaHandlerServesOpenedInodeAfterPathReplacement(t *testing.T) {
	dataDir := t.TempDir()
	writeQCamera(t, dataDir, "dongle", "route", []byte("inside"))
	target := filepath.Join(dataDir, "uploads", "dongle", "route", "0", "qcamera.ts")
	outside := filepath.Join(t.TempDir(), "qcamera.ts")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := NewTicketManager("secret", time.Minute)
	token, _, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	media := NewMediaHandler(manager, &stubSegmentProvider{segments: []Segment{{
		Number: 0, Duration: 1, QCameraRelPath: "route/0/qcamera.ts",
	}}}, NewLocator(dataDir))
	media.afterMediaOpen = func() {
		if err := os.Remove(target); err != nil {
			panic(err)
		}
		if err := os.Symlink(outside, target); err != nil {
			panic(err)
		}
	}
	mux := http.NewServeMux()
	media.Mount(mux)

	response := serveMediaRequest(mux, http.MethodGet, "/media/hls/"+token+"/0.ts", "")
	if response.Code != http.StatusOK || response.Body.String() != "inside" {
		t.Fatalf("status/body = %d/%q, want opened inode", response.Code, response.Body.String())
	}
}

func TestMediaHandlerPlaylistUsesEnrichedDurationsButTSUsesPlainSegments(t *testing.T) {
	dataDir := t.TempDir()
	writeQCamera(t, dataDir, "dongle", "route", []byte("video"))
	manager := NewTicketManager("secret", time.Minute)
	token, _, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := &enrichedSegmentProvider{
		plain: []Segment{{
			Number: 0, Duration: 60, QCameraRelPath: "route/0/qcamera.ts",
		}},
		enriched: []Segment{{
			Number: 0, Duration: 12.5, QCameraRelPath: "route/0/qcamera.ts",
		}},
	}
	handler := newTestMediaHandler(manager, provider, dataDir)

	playlist := serveMediaRequest(handler, http.MethodGet, "/media/hls/"+token+"/index.m3u8", "")
	if playlist.Code != http.StatusOK || !strings.Contains(playlist.Body.String(), "#EXTINF:12.500,") {
		t.Fatalf("playlist status/body = %d/%q", playlist.Code, playlist.Body.String())
	}
	if provider.enrichedCalls != 1 || provider.plainCalls != 0 {
		t.Fatalf("playlist calls plain/enriched = %d/%d", provider.plainCalls, provider.enrichedCalls)
	}

	segment := serveMediaRequest(handler, http.MethodGet, "/media/hls/"+token+"/0.ts", "")
	if segment.Code != http.StatusOK || segment.Body.String() != "video" {
		t.Fatalf("segment status/body = %d/%q", segment.Code, segment.Body.String())
	}
	if provider.enrichedCalls != 1 || provider.plainCalls != 1 {
		t.Fatalf("TS calls plain/enriched = %d/%d", provider.plainCalls, provider.enrichedCalls)
	}
}

func TestMediaHandlerSegmentPlaylistUsesOnlyScopedEnrichment(t *testing.T) {
	manager := NewTicketManager("secret", time.Minute)
	selected := 1
	token, _, err := manager.Issue("dongle", "route", TicketModeSegment, &selected)
	if err != nil {
		t.Fatal(err)
	}
	provider := &enrichedSegmentProvider{
		plain: []Segment{
			{Number: 0, Duration: 60, QCameraRelPath: "route/0/qcamera.ts"},
			{Number: 1, Duration: 60, QCameraRelPath: "route/1/qcamera.ts"},
		},
		enriched: []Segment{
			{Number: 0, Duration: 10, QCameraRelPath: "route/0/qcamera.ts"},
			{Number: 1, Duration: 20, QCameraRelPath: "route/1/qcamera.ts"},
		},
		scoped: Segment{Number: 1, Duration: 20, QCameraRelPath: "route/1/qcamera.ts"},
	}
	handler := newTestMediaHandler(manager, provider, t.TempDir())

	response := serveMediaRequest(handler, http.MethodGet,
		"/media/hls/"+token+"/index.m3u8", "")
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), "#EXTINF:20.000,") ||
		strings.Contains(response.Body.String(), "0.ts") {
		t.Fatalf("status/body = %d/%q", response.Code, response.Body.String())
	}
	if provider.scopedCalls != 1 || provider.enrichedCalls != 0 || provider.plainCalls != 0 {
		t.Fatalf("calls plain/full/scoped = %d/%d/%d",
			provider.plainCalls, provider.enrichedCalls, provider.scopedCalls)
	}
}

func TestMediaHandlerScopedPlaylistReturnsNotFoundAfterDeletion(t *testing.T) {
	tests := []struct {
		name      string
		remaining []store.Segment
	}{
		{
			name: "segment deleted",
			remaining: []store.Segment{
				{RouteName: "route", SegmentName: "0", RelPath: "route/0/qcamera.ts"},
			},
		},
		{name: "route deleted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := NewTicketManager("secret", time.Minute)
			selected := 1
			mediaStore := &mutableMediaStore{segments: []store.Segment{
				{RouteName: "route", SegmentName: "0", RelPath: "route/0/qcamera.ts"},
				{RouteName: "route", SegmentName: "1", RelPath: "route/1/qcamera.ts"},
			}}
			service := NewService(mediaStore, manager)
			token, _, err := service.IssueMediaTicket(
				"dongle", "route", TicketModeSegment, &selected,
			)
			if err != nil {
				t.Fatal(err)
			}
			mediaStore.segments = test.remaining
			handler := newTestMediaHandler(manager, service, t.TempDir())

			response := serveMediaRequest(handler, http.MethodGet,
				"/media/hls/"+token+"/index.m3u8", "")
			if response.Code != http.StatusNotFound {
				t.Fatalf("status/body = %d/%q, want 404", response.Code, response.Body.String())
			}
		})
	}
}

func TestMediaHandlerRoutePlaylistSkipsFileDeletedAfterTicket(t *testing.T) {
	dataDir := t.TempDir()
	for _, number := range []int{0, 1} {
		dir := filepath.Join(dataDir, "uploads", "dongle", "route", strconv.Itoa(number))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "qcamera.ts"), []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manager := NewTicketManager("secret", time.Minute)
	service := NewServiceWithTelemetry(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qcamera.ts"},
		{RouteName: "route", SegmentName: "1", RelPath: "route/1/qcamera.ts"},
	}}, manager, NewLocator(dataDir), NewCache(dataDir, &cacheParser{}))
	token, _, err := service.IssueMediaTicket("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dataDir, "uploads", "dongle", "route", "0", "qcamera.ts")); err != nil {
		t.Fatal(err)
	}
	handler := newTestMediaHandler(manager, service, dataDir)

	response := serveMediaRequest(handler, http.MethodGet,
		"/media/hls/"+token+"/index.m3u8", "")
	if response.Code != http.StatusOK ||
		strings.Contains(response.Body.String(), "\n0.ts\n") ||
		!strings.Contains(response.Body.String(), "\n1.ts\n") {
		t.Fatalf("status/body = %d/%q", response.Code, response.Body.String())
	}
}

func TestMediaHandlerTSWithServiceDoesNotParseTelemetry(t *testing.T) {
	dataDir := t.TempDir()
	writeQCamera(t, dataDir, "dongle", "route", []byte("video"))
	writeServiceQlog(t, dataDir, "dongle", "route", 0)
	parser := &recordingSegmentParser{}
	manager := NewTicketManager("secret", time.Minute)
	service := NewServiceWithTelemetry(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qcamera.ts"},
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qlog.zst"},
	}}, manager, NewLocator(dataDir), NewCache(dataDir, parser))
	token, _, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := newTestMediaHandler(manager, service, dataDir)

	response := serveMediaRequest(handler, http.MethodGet, "/media/hls/"+token+"/0.ts", "")
	if response.Code != http.StatusOK || response.Body.String() != "video" {
		t.Fatalf("status/body = %d/%q", response.Code, response.Body.String())
	}
	if len(parser.segments) != 0 {
		t.Fatalf("TS parsed telemetry segments = %v", parser.segments)
	}
}

func TestMediaHandlerPlaylistsNeverParseTelemetryAndUseValidCache(t *testing.T) {
	dataDir := t.TempDir()
	writeQCamera(t, dataDir, "dongle", "route", []byte("video"))
	writeServiceQlog(t, dataDir, "dongle", "route", 0)
	parser := &cacheParser{result: SegmentTelemetry{Segment: 0, Duration: 12.5}}
	manager := NewTicketManager("secret", time.Minute)
	service := NewServiceWithTelemetry(serviceSegmentStore{segments: []store.Segment{
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qcamera.ts"},
		{RouteName: "route", SegmentName: "0", RelPath: "route/0/qlog.zst"},
	}}, manager, NewLocator(dataDir), NewCache(dataDir, parser))
	handler := newTestMediaHandler(manager, service, dataDir)
	selected := 0
	routeToken, _, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	segmentToken, _, err := manager.Issue("dongle", "route", TicketModeSegment, &selected)
	if err != nil {
		t.Fatal(err)
	}

	for _, token := range []string{routeToken, routeToken, segmentToken, segmentToken} {
		response := serveMediaRequest(handler, http.MethodGet,
			"/media/hls/"+token+"/index.m3u8", "")
		if response.Code != http.StatusOK ||
			!strings.Contains(response.Body.String(), "#EXTINF:60.000,") {
			t.Fatalf("cache-miss playlist status/body = %d/%q", response.Code, response.Body.String())
		}
	}
	if parser.callCount() != 0 {
		t.Fatalf("cache-miss playlist parser calls = %d, want 0", parser.callCount())
	}

	if _, err := service.Telemetry("dongle", "route", 0); err != nil {
		t.Fatal(err)
	}
	if parser.callCount() != 1 {
		t.Fatalf("admin telemetry parser calls = %d, want 1", parser.callCount())
	}
	for _, token := range []string{routeToken, segmentToken} {
		response := serveMediaRequest(handler, http.MethodGet,
			"/media/hls/"+token+"/index.m3u8", "")
		if response.Code != http.StatusOK ||
			!strings.Contains(response.Body.String(), "#EXTINF:12.500,") {
			t.Fatalf("cache-hit playlist status/body = %d/%q", response.Code, response.Body.String())
		}
	}
	if parser.callCount() != 1 {
		t.Fatalf("cache-hit playlist parser calls = %d, want 1", parser.callCount())
	}

	qlog := filepath.Join(dataDir, "uploads", "dongle", "route", "0", "qlog.zst")
	if err := os.WriteFile(qlog, []byte("changed source"), 0o600); err != nil {
		t.Fatal(err)
	}
	response := serveMediaRequest(handler, http.MethodGet,
		"/media/hls/"+routeToken+"/index.m3u8", "")
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), "#EXTINF:60.000,") {
		t.Fatalf("changed-source playlist status/body = %d/%q", response.Code, response.Body.String())
	}
	if parser.callCount() != 1 {
		t.Fatalf("changed-source playlist parser calls = %d, want 1", parser.callCount())
	}
}

func TestMediaHandlerRejectsOutOfScopeSegment(t *testing.T) {
	manager := NewTicketManager("secret", time.Minute)
	selected := 0
	token, _, err := manager.Issue("dongle", "route", TicketModeSegment, &selected)
	if err != nil {
		t.Fatal(err)
	}
	provider := &stubSegmentProvider{segments: []Segment{
		{Number: 0, Duration: 1, QCameraRelPath: "route/0/qcamera.ts"},
		{Number: 1, Duration: 1, QCameraRelPath: "route/1/qcamera.ts"},
	}, err: errors.New("database failed")}
	handler := newTestMediaHandler(manager, provider, t.TempDir())

	response := serveMediaRequest(handler, http.MethodGet, "/media/hls/"+token+"/1.ts", "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls)
	}
}

func TestMediaHandlerReturnsNotFoundForMissingMedia(t *testing.T) {
	manager := NewTicketManager("secret", time.Minute)
	token, _, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		path     string
		segments []Segment
	}{
		{name: "missing segment", path: "/2.ts", segments: []Segment{{Number: 0, Duration: 1, QCameraRelPath: "route/0/qcamera.ts"}}},
		{name: "missing qcamera", path: "/0.ts", segments: []Segment{{Number: 0, Duration: 1}}},
		{name: "playlist has no qcamera", path: "/index.m3u8", segments: []Segment{{Number: 0, Duration: 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestMediaHandler(manager, &stubSegmentProvider{segments: tt.segments}, t.TempDir())
			response := serveMediaRequest(handler, http.MethodGet, "/media/hls/"+token+tt.path, "")
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", response.Code)
			}
		})
	}
}

func TestMediaHandlerReturnsUnauthorizedBeforeProvider(t *testing.T) {
	provider := &stubSegmentProvider{}
	manager := NewTicketManager("secret", time.Minute)
	handler := newTestMediaHandler(manager, provider, t.TempDir())

	for _, token := range []string{"invalid", issueExpiredTicket(t)} {
		response := serveMediaRequest(handler, http.MethodGet, "/media/hls/"+token+"/index.m3u8", "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", response.Code)
		}
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls)
	}
}

func TestMediaHandlerReturnsInternalServerErrorForProviderError(t *testing.T) {
	manager := NewTicketManager("secret", time.Minute)
	token, _, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := &stubSegmentProvider{err: errors.New("database failed")}
	handler := newTestMediaHandler(manager, provider, t.TempDir())

	response := serveMediaRequest(handler, http.MethodGet, "/media/hls/"+token+"/index.m3u8", "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
}

func TestMediaHandlerMapsProviderNotFoundErrorsToNotFound(t *testing.T) {
	manager := NewTicketManager("secret", time.Minute)
	token, _, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, providerErr := range []error{ErrRouteNotFound, ErrMediaNotFound} {
		handler := newTestMediaHandler(manager, &stubSegmentProvider{err: providerErr}, t.TempDir())
		for _, suffix := range []string{"/index.m3u8", "/0.ts"} {
			response := serveMediaRequest(handler, http.MethodGet,
				"/media/hls/"+token+suffix, "")
			if response.Code != http.StatusNotFound {
				t.Fatalf("error %v path %s status = %d, want 404",
					providerErr, suffix, response.Code)
			}
		}
	}
}

func TestMediaHandlerRejectsMalformedTSName(t *testing.T) {
	manager := NewTicketManager("secret", time.Minute)
	token, _, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := &stubSegmentProvider{err: errors.New("database failed")}
	handler := newTestMediaHandler(manager, provider, t.TempDir())

	for _, name := range []string{"-1.ts", "1.m4s", "abc.ts"} {
		response := serveMediaRequest(handler, http.MethodGet, "/media/hls/"+token+"/"+name, "")
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", name, response.Code)
		}
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls)
	}
}

func newTestMediaHandler(manager *TicketManager, provider SegmentProvider, dataDir string) http.Handler {
	mux := http.NewServeMux()
	NewMediaHandler(manager, provider, NewLocator(dataDir)).Mount(mux)
	return mux
}

func serveMediaRequest(handler http.Handler, method, target, byteRange string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	if byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func writeQCamera(t *testing.T, dataDir, dongleID, route string, contents []byte) {
	t.Helper()
	dir := filepath.Join(dataDir, "uploads", dongleID, route, "0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "qcamera.ts"), contents, 0o644); err != nil {
		t.Fatal(err)
	}
}

func issueExpiredTicket(t *testing.T) string {
	t.Helper()
	manager := NewTicketManager("secret", -time.Minute)
	token, _, err := manager.Issue("dongle", "route", TicketModeRoute, nil)
	if err != nil {
		t.Fatal(err)
	}
	return token
}
