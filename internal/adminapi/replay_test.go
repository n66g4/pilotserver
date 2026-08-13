package adminapi_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"pilotserver/internal/athena"
	"pilotserver/internal/auth"
	"pilotserver/internal/config"
	"pilotserver/internal/replay"
	"pilotserver/internal/store"
)

type adminReplayFixture struct {
	mux     *http.ServeMux
	store   *store.Store
	tickets *replay.TicketManager
	token   string
	dataDir string
}

func newAdminReplayFixture(t *testing.T) adminReplayFixture {
	return newAdminReplayFixtureWithParser(t, &adminTelemetryParser{
		result: replay.SegmentTelemetry{Segment: 2, Duration: 12.5},
	})
}

type adminTelemetryParser struct {
	result replay.SegmentTelemetry
	err    error
}

func (p *adminTelemetryParser) ParseSegment(io.Reader, int) (replay.SegmentTelemetry, error) {
	return p.result, p.err
}

func newAdminReplayFixtureWithParser(t *testing.T, parser replay.SegmentParser) adminReplayFixture {
	t.Helper()
	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	passwordHash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.IssueAdminJWT(adminTestSecret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	tickets := replay.NewTicketManager(adminTestSecret, 15*time.Minute)
	cfg := config.Config{
		JWTSecret: adminTestSecret,
		DataDir:   dataDir,
	}
	service := replay.NewServiceWithTelemetry(
		st, tickets, replay.NewLocator(dataDir), replay.NewCache(dataDir, parser),
	)
	mountAdminWithReplayService(t, mux, st, athena.NewHub(), cfg, passwordHash, service)
	return adminReplayFixture{
		mux: mux, store: st, tickets: tickets, token: token, dataDir: dataDir,
	}
}

func (f adminReplayFixture) insert(t *testing.T, segment store.Segment) {
	t.Helper()
	segment.DongleID = "d1"
	segment.RouteName = "route"
	if err := f.store.InsertSegment(segment); err != nil {
		t.Fatal(err)
	}
}

func (f adminReplayFixture) insertQlog(t *testing.T, segment int) {
	t.Helper()
	f.insert(t, store.Segment{
		SegmentName: strconv.Itoa(segment),
		RelPath:     "route/" + strconv.Itoa(segment) + "/qlog.zst",
	})
	dir := filepath.Join(f.dataDir, "uploads", "d1", "route", strconv.Itoa(segment))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "qlog.zst"), []byte("qlog"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f adminReplayFixture) insertQCamera(t *testing.T, segment int) {
	t.Helper()
	f.insert(t, store.Segment{
		SegmentName: strconv.Itoa(segment),
		RelPath:     "route/" + strconv.Itoa(segment) + "/qcamera.ts",
	})
	dir := filepath.Join(f.dataDir, "uploads", "d1", "route", strconv.Itoa(segment))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "qcamera.ts"), []byte("video"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f adminReplayFixture) request(method, target, body string, authorized bool) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	if authorized {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	f.mux.ServeHTTP(rec, req)
	return rec
}

func TestAdminReplayRequiresAuthentication(t *testing.T) {
	fixture := newAdminReplayFixture(t)
	for _, request := range []struct {
		method string
		target string
	}{
		{method: http.MethodGet, target: "/admin/api/devices/d1/routes/route/replay"},
		{method: http.MethodGet, target: "/admin/api/devices/d1/routes/route/segments/2/telemetry"},
		{method: http.MethodPost, target: "/admin/api/devices/d1/routes/route/media-ticket"},
	} {
		rec := fixture.request(request.method, request.target, "", false)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status = %d, want 401", request.method, request.target, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/admin/api/devices/d1/routes/route/segments/2/telemetry", nil)
	req.Header.Set("Authorization", "Bearer invalid")
	fixture.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token status = %d, want 401", rec.Code)
	}
}

func TestAdminTelemetryReturnsAuthenticatedJSON(t *testing.T) {
	fixture := newAdminReplayFixture(t)
	fixture.insertQlog(t, 2)

	rec := fixture.request(http.MethodGet,
		"/admin/api/devices/d1/routes/route/segments/2/telemetry", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var telemetry replay.SegmentTelemetry
	if err := json.NewDecoder(rec.Body).Decode(&telemetry); err != nil {
		t.Fatal(err)
	}
	if telemetry.Segment != 2 || telemetry.Duration != 12.5 {
		t.Fatalf("telemetry = %+v", telemetry)
	}
}

func TestAdminTelemetryRejectsInvalidSegmentSyntax(t *testing.T) {
	fixture := newAdminReplayFixture(t)
	for _, segment := range []string{
		"-1", "+1", "%201", "1x", "9223372036854775808", "999999999999999999999999999",
	} {
		rec := fixture.request(http.MethodGet,
			"/admin/api/devices/d1/routes/route/segments/"+segment+"/telemetry", "", true)
		if rec.Code != http.StatusBadRequest || rec.Body.String() != "invalid segment\n" {
			t.Fatalf("segment %q status/body = %d/%q", segment, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminTelemetryMapsParserInvalidSegmentToBadRequest(t *testing.T) {
	fixture := newAdminReplayFixtureWithParser(t, replay.NewParser())
	const segment = 2_147_483_648
	fixture.insertQlog(t, segment)

	rec := fixture.request(http.MethodGet,
		"/admin/api/devices/d1/routes/route/segments/2147483648/telemetry", "", true)
	if rec.Code != http.StatusBadRequest || rec.Body.String() != "invalid segment\n" {
		t.Fatalf("status/body = %d/%q, want 400 invalid segment", rec.Code, rec.Body.String())
	}
}

func TestAdminTelemetryMapsMissingToNotFound(t *testing.T) {
	fixture := newAdminReplayFixture(t)
	fixture.insert(t, store.Segment{
		SegmentName: "2", RelPath: "route/2/qcamera.ts",
	})
	rec := fixture.request(http.MethodGet,
		"/admin/api/devices/d1/routes/route/segments/2/telemetry", "", true)
	if rec.Code != http.StatusNotFound || rec.Body.String() != "not found\n" {
		t.Fatalf("status/body = %d/%q", rec.Code, rec.Body.String())
	}
}

func TestAdminTelemetryMapsParserAndCacheErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{name: "zstd", err: replay.ErrZstd, status: 422, body: "telemetry unavailable\n"},
		{name: "capnp", err: replay.ErrCapnp, status: 422, body: "telemetry unavailable\n"},
		{name: "decompressed limit", err: replay.ErrDecompressedLimit, status: 422, body: "telemetry unavailable\n"},
		{name: "event limit", err: replay.ErrEventLimit, status: 422, body: "telemetry unavailable\n"},
		{name: "invalid limits", err: replay.ErrInvalidLimits, status: 422, body: "telemetry unavailable\n"},
		{name: "source changed", err: replay.ErrTelemetrySourceChanged, status: 409, body: "telemetry source changed\n"},
		{name: "internal path hidden", err: errors.New("/private/uploads/secret/qlog.zst"), status: 500, body: "telemetry failed\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAdminReplayFixtureWithParser(t, &adminTelemetryParser{err: test.err})
			fixture.insertQlog(t, 2)
			rec := fixture.request(http.MethodGet,
				"/admin/api/devices/d1/routes/route/segments/2/telemetry", "", true)
			if rec.Code != test.status || rec.Body.String() != test.body {
				t.Fatalf("status/body = %d/%q", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "/private/") {
				t.Fatalf("response leaked path: %q", rec.Body.String())
			}
		})
	}
}

func TestAdminReplayReturnsSummary(t *testing.T) {
	fixture := newAdminReplayFixture(t)
	fixture.insertQCamera(t, 2)
	fixture.insertQlog(t, 2)

	rec := fixture.request(http.MethodGet, "/admin/api/devices/d1/routes/route/replay", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	var summary replay.ReplaySummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if summary.Route != "route" || summary.Duration != 12.5 || len(summary.Segments) != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	segment := summary.Segments[0]
	if segment.Number != 2 || !segment.HasVideo || !segment.HasTelemetry ||
		segment.DurationEstimated || segment.TelemetryError != "" {
		t.Fatalf("segment = %+v", segment)
	}
}

func TestAdminReplayReturnsSummaryForDragonPilotTwoLevelPaths(t *testing.T) {
	fixture := newAdminReplayFixture(t)
	const route = "00000010--2cbbf69c9f"
	for _, filename := range []string{"qcamera.ts", "qlog.zst"} {
		relPath := route + "--2/" + filename
		if err := fixture.store.InsertSegment(store.Segment{
			DongleID: "d1", RouteName: route, SegmentName: "2", RelPath: relPath,
		}); err != nil {
			t.Fatal(err)
		}
		fullPath := filepath.Join(fixture.dataDir, "uploads", "d1", filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(filename), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	rec := fixture.request(http.MethodGet,
		"/admin/api/devices/d1/routes/"+route+"/replay", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var summary replay.ReplaySummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Segments) != 1 ||
		!summary.Segments[0].HasVideo ||
		!summary.Segments[0].HasTelemetry {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestAdminReplayReturnsEmptySummaryForBootQlog(t *testing.T) {
	fixture := newAdminReplayFixture(t)
	if err := fixture.store.InsertSegment(store.Segment{
		DongleID: "d1", RouteName: "boot", SegmentName: "boot", RelPath: "boot/qlog.zst",
	}); err != nil {
		t.Fatal(err)
	}

	rec := fixture.request(http.MethodGet,
		"/admin/api/devices/d1/routes/boot/replay", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var summary replay.ReplaySummary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if summary.Route != "boot" || summary.Duration != 0 || len(summary.Segments) != 0 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestAdminMediaTicketIssuesRouteAndSegmentTickets(t *testing.T) {
	fixture := newAdminReplayFixture(t)
	fixture.insertQCamera(t, 2)

	tests := []struct {
		name    string
		body    string
		mode    replay.TicketMode
		segment *int
	}{
		{name: "route", body: `{"mode":"route"}`, mode: replay.TicketModeRoute},
		{name: "segment", body: `{"mode":"segment","segment":2}`, mode: replay.TicketModeSegment, segment: intPointer(2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := fixture.request(http.MethodPost,
				"/admin/api/devices/d1/routes/route/media-ticket", tt.body, true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q", got)
			}
			var response struct {
				PlaylistURL string    `json:"playlist_url"`
				ExpiresAt   time.Time `json:"expires_at"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
				t.Fatal(err)
			}
			token := strings.TrimSuffix(strings.TrimPrefix(
				response.PlaylistURL, "/media/hls/"), "/index.m3u8")
			token, err := url.PathUnescape(token)
			if err != nil {
				t.Fatal(err)
			}
			ticket, err := fixture.tickets.Verify(token)
			if err != nil {
				t.Fatal(err)
			}
			if ticket.Mode != tt.mode || !sameIntPointer(ticket.Segment, tt.segment) ||
				!ticket.ExpiresAt.Equal(response.ExpiresAt) {
				t.Fatalf("ticket = %+v, response expiry = %v", ticket, response.ExpiresAt)
			}
		})
	}
}

func TestAdminMediaTicketRejectsInvalidRequests(t *testing.T) {
	fixture := newAdminReplayFixture(t)
	fixture.insert(t, store.Segment{SegmentName: "2", RelPath: "route/2/qcamera.ts"})

	for _, body := range []string{
		`{`,
		`{"mode":"route"} garbage`,
		`{"mode":"unknown"}`,
		`{"mode":"route","segment":2}`,
		`{"mode":"segment"}`,
	} {
		rec := fixture.request(http.MethodPost,
			"/admin/api/devices/d1/routes/route/media-ticket", body, true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q status = %d, response = %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminReplayAndMediaTicketMapNotFound(t *testing.T) {
	fixture := newAdminReplayFixture(t)

	rec := fixture.request(http.MethodGet,
		"/admin/api/devices/d1/routes/missing/replay", "", true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown route status = %d, body = %s", rec.Code, rec.Body.String())
	}

	fixture.insert(t, store.Segment{SegmentName: "2", RelPath: "route/2/qlog.zst"})
	for name, body := range map[string]string{
		"route without video": `{"mode":"route"}`,
		"unknown segment":     `{"mode":"segment","segment":3}`,
		"segment no video":    `{"mode":"segment","segment":2}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := fixture.request(http.MethodPost,
				"/admin/api/devices/d1/routes/route/media-ticket", body, true)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func intPointer(value int) *int {
	return &value
}

func sameIntPointer(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
