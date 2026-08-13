package store_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"pilotserver/internal/store"
)

func TestUpsertAndGetDevice(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	d := store.Device{
		DongleID:     "testdongle001",
		PublicKeyPEM: "-----BEGIN PUBLIC KEY-----\nMIIB\n-----END PUBLIC KEY-----",
		Alias:        "car1",
		CreatedAt:    time.Now().Unix(),
	}
	if err := s.UpsertDevice(d); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDevice("testdongle001")
	if err != nil {
		t.Fatal(err)
	}
	if got.DongleID != d.DongleID || got.Alias != "car1" {
		t.Fatalf("got %+v", got)
	}
}

func TestListSegmentsWhileUploading(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	const operations = 200
	errs := make(chan error, operations*2)
	var wg sync.WaitGroup
	for i := 0; i < operations; i++ {
		i := i
		wg.Add(2)
		go func() {
			defer wg.Done()
			errs <- s.InsertSegment(store.Segment{
				DongleID:    "device",
				RouteName:   "route",
				SegmentName: fmt.Sprintf("%03d", i),
				RelPath:     fmt.Sprintf("route/%03d/qlog.zst", i),
				Size:        int64(i),
			})
		}()
		go func() {
			defer wg.Done()
			_, err := s.ListSegments("device", "route")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent segment access failed: %v", err)
		}
	}
}

func TestOpenMigratesTwoLevelDragonPilotMetadata(t *testing.T) {
	dataDir := t.TempDir()
	db := openLegacyRouteDB(t, dataDir)
	insertLegacySegment(t, db, "dongle", "route--with--parts--0/qcamera.ts", 300)
	insertLegacySegment(t, db, "dongle", "route--with--parts--12/qlog.zst", 200)
	insertLegacySegment(t, db, "dongle", "boot/qlog.zst", 10)
	for route, createdAt := range map[string]int64{
		"route--with--parts--0":  15,
		"route--with--parts--12": 5,
	} {
		if _, err := db.Exec(
			`UPDATE routes SET created_at = ? WHERE dongle_id = ? AND name = ?`,
			createdAt, "dongle", route,
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO routes (dongle_id, name, created_at) VALUES (?, ?, ?)`,
		"dongle", "route--with--parts", 25); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := st.ListRoutes("dongle")
	if err != nil {
		t.Fatal(err)
	}
	wantRoutes := []store.Route{
		{DongleID: "dongle", Name: "boot", CreatedAt: 10},
		{DongleID: "dongle", Name: "route--with--parts", CreatedAt: 5},
	}
	if !reflect.DeepEqual(routes, wantRoutes) {
		t.Fatalf("routes = %+v, want %+v", routes, wantRoutes)
	}
	segments, err := st.ListSegments("dongle", "route--with--parts")
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 ||
		segments[0].SegmentName != "0" ||
		segments[1].SegmentName != "12" ||
		segments[0].RelPath != "route--with--parts--0/qcamera.ts" ||
		segments[1].RelPath != "route--with--parts--12/qlog.zst" {
		t.Fatalf("canonical segments = %+v", segments)
	}
	boot, err := st.ListSegments("dongle", "boot")
	if err != nil {
		t.Fatal(err)
	}
	if len(boot) != 1 || boot[0].RouteName != "boot" {
		t.Fatalf("boot segments = %+v", boot)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = store.Open(dataDir)
	if err != nil {
		t.Fatalf("idempotent reopen failed: %v", err)
	}
	defer st.Close()
	routesAgain, err := st.ListRoutes("dongle")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(routesAgain, wantRoutes) {
		t.Fatalf("routes after second migration = %+v, want %+v", routesAgain, wantRoutes)
	}
}

func TestOpenRollsBackRouteMetadataMigration(t *testing.T) {
	dataDir := t.TempDir()
	db := openLegacyRouteDB(t, dataDir)
	insertLegacySegment(t, db, "dongle", "route--0/qcamera.ts", 10)
	insertLegacySegment(t, db, "dongle", "route--1/qcamera.ts", 20)
	if _, err := db.Exec(`
		CREATE TRIGGER fail_second_segment
		BEFORE UPDATE OF route_name ON segments
		WHEN NEW.segment_name = '1'
		BEGIN
		  SELECT RAISE(ABORT, 'injected migration failure');
		END`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if st, err := store.Open(dataDir); err == nil {
		st.Close()
		t.Fatal("Open() succeeded despite injected migration failure")
	}

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "pilotserver.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT route_name, segment_name FROM segments ORDER BY rel_path`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got [][2]string
	for rows.Next() {
		var values [2]string
		if err := rows.Scan(&values[0], &values[1]); err != nil {
			t.Fatal(err)
		}
		got = append(got, values)
	}
	want := [][2]string{{"route--0", "route--0"}, {"route--1", "route--1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata after rollback = %+v, want %+v", got, want)
	}
}

func openLegacyRouteDB(t *testing.T, dataDir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "pilotserver.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		CREATE TABLE routes (
		  dongle_id TEXT NOT NULL,
		  name TEXT NOT NULL,
		  created_at INTEGER NOT NULL,
		  PRIMARY KEY (dongle_id, name)
		);
		CREATE TABLE segments (
		  dongle_id TEXT NOT NULL,
		  route_name TEXT NOT NULL,
		  segment_name TEXT NOT NULL,
		  rel_path TEXT NOT NULL,
		  size INTEGER NOT NULL,
		  uploaded_at INTEGER NOT NULL,
		  PRIMARY KEY (dongle_id, rel_path)
		)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}

func insertLegacySegment(t *testing.T, db *sql.DB, dongleID, relPath string, uploadedAt int64) {
	t.Helper()
	routeName := strings.Split(relPath, "/")[0]
	if _, err := db.Exec(`INSERT INTO routes (dongle_id, name, created_at) VALUES (?, ?, ?)`,
		dongleID, routeName, uploadedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO segments
		  (dongle_id, route_name, segment_name, rel_path, size, uploaded_at)
		VALUES (?, ?, ?, ?, 1, ?)`,
		dongleID, routeName, routeName, relPath, uploadedAt); err != nil {
		t.Fatal(err)
	}
}

func TestMapSettingsDefaultAndRoundTrip(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	got, err := s.GetMapSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got != (store.MapSettings{Provider: "none"}) {
		t.Fatalf("default map settings = %+v", got)
	}

	want := store.MapSettings{
		Provider:     "amap",
		WebKey:       "web-key",
		SecurityCode: "security-code",
	}
	if err := s.SetMapSettings(want); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetMapSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("map settings = %+v, want %+v", got, want)
	}
}

func TestUpdateMapSettingsPreservesConcurrentPartialUpdates(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetMapSettings(store.MapSettings{Provider: "amap"}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	updates := []func(store.MapSettings) store.MapSettings{
		func(settings store.MapSettings) store.MapSettings {
			settings.WebKey = "concurrent-key"
			return settings
		},
		func(settings store.MapSettings) store.MapSettings {
			settings.SecurityCode = "concurrent-code"
			return settings
		},
	}
	for _, update := range updates {
		update := update
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.UpdateMapSettings(func(settings store.MapSettings) (store.MapSettings, error) {
				return update(settings), nil
			})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.GetMapSettings()
	if err != nil {
		t.Fatal(err)
	}
	want := store.MapSettings{
		Provider:     "amap",
		WebKey:       "concurrent-key",
		SecurityCode: "concurrent-code",
	}
	if got != want {
		t.Fatalf("map settings = %+v, want %+v", got, want)
	}
}
