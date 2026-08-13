package replay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"pilotserver/internal/replay/cereal"
)

type cacheParser struct {
	mu      sync.Mutex
	calls   int
	result  SegmentTelemetry
	err     error
	started chan struct{}
	release chan struct{}
	onParse func()
}

type contentTelemetryParser struct{}

func (c *Cache) Load(dongleID, route string, segment int, qlogPath string) (SegmentTelemetry, error) {
	source, err := openCacheTestSource(qlogPath)
	if err != nil {
		return SegmentTelemetry{}, err
	}
	return c.LoadFile(dongleID, route, segment, source)
}

func openCacheTestSource(name string) (*LocatedFile, error) {
	before, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		file.Close()
		return nil, ErrTelemetrySourceChanged
	}
	return &LocatedFile{
		File: file,
		info: opened,
		reopen: func() (*LocatedFile, error) {
			return openCacheTestSource(name)
		},
	}, nil
}

func (contentTelemetryParser) ParseSegment(r io.Reader, segment int) (SegmentTelemetry, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return SegmentTelemetry{}, err
	}
	return SegmentTelemetry{Segment: segment, Duration: float64(len(data))}, nil
}

func (p *cacheParser) ParseSegment(io.Reader, int) (SegmentTelemetry, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	if p.started != nil {
		select {
		case p.started <- struct{}{}:
		default:
		}
	}
	if p.onParse != nil {
		p.onParse()
	}
	if p.release != nil {
		<-p.release
	}
	return p.result, p.err
}

func (p *cacheParser) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestCacheWritesThenHits(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "qlog")
	parser := &cacheParser{result: testTelemetry(2)}
	cache := NewCache(dataDir, parser)

	first, err := cache.Load("dongle", "route", 2, qlog)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.Load("dongle", "route", 2, qlog)
	if err != nil {
		t.Fatal(err)
	}
	if parser.callCount() != 1 || first.Duration != 12.5 || second.Duration != 12.5 {
		t.Fatalf("calls/results = %d/%+v/%+v", parser.callCount(), first, second)
	}
	envelope := readCacheEnvelope(t, cachePath(dataDir, "dongle", "route", 2))
	if envelope.Version != CacheVersion || envelope.SchemaVersion != cereal.SchemaVersion ||
		envelope.Telemetry.Segment != 2 || envelope.Source.Size != int64(len("qlog")) {
		t.Fatalf("cache envelope = %+v", envelope)
	}
}

func TestCacheVersionTwoMissesV1AndThenHitsV2(t *testing.T) {
	if CacheVersion != 2 {
		t.Fatalf("CacheVersion = %d, want 2", CacheVersion)
	}
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "qlog")
	info, err := os.Stat(qlog)
	if err != nil {
		t.Fatal(err)
	}
	v1Path := filepath.Join(dataDir, "replay-cache", "dongle", "route", "0.v1.json")
	if err := os.MkdirAll(filepath.Dir(v1Path), 0o700); err != nil {
		t.Fatal(err)
	}
	v1Data, err := json.Marshal(cacheEnvelope{
		Version: 1, SchemaVersion: cereal.SchemaVersion,
		Source: fingerprintOf(info), Telemetry: testTelemetry(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(v1Path, v1Data, 0o600); err != nil {
		t.Fatal(err)
	}

	parser := &cacheParser{result: testTelemetry(0)}
	cache := NewCache(dataDir, parser)
	if _, err := cache.Load("dongle", "route", 0, qlog); err != nil {
		t.Fatal(err)
	}
	if parser.callCount() != 1 {
		t.Fatalf("v1 cache parser calls = %d, want reparse", parser.callCount())
	}
	if _, err := os.Stat(cachePath(dataDir, "dongle", "route", 0)); err != nil {
		t.Fatalf("v2 cache was not written: %v", err)
	}
	if _, err := cache.Load("dongle", "route", 0, qlog); err != nil {
		t.Fatal(err)
	}
	if parser.callCount() != 1 {
		t.Fatalf("v2 cache parser calls = %d, want hit without reparse", parser.callCount())
	}
}

func TestCacheInvalidatesSourceFingerprint(t *testing.T) {
	for _, name := range []string{"size", "mtime"} {
		t.Run(name, func(t *testing.T) {
			dataDir := t.TempDir()
			qlog := writeCacheQlog(t, dataDir, "old")
			parser := &cacheParser{result: testTelemetry(0)}
			cache := NewCache(dataDir, parser)
			if _, err := cache.Load("dongle", "route", 0, qlog); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(qlog)
			if err != nil {
				t.Fatal(err)
			}
			if name == "size" {
				if err := os.WriteFile(qlog, []byte("new-size"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Chtimes(qlog, info.ModTime().Add(time.Second), info.ModTime().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.Load("dongle", "route", 0, qlog); err != nil {
				t.Fatal(err)
			}
			if parser.callCount() != 2 {
				t.Fatalf("parser calls = %d, want 2", parser.callCount())
			}
		})
	}
}

func TestCacheRebuildsInvalidFiles(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*cacheEnvelope)
		raw    string
	}{
		{name: "wrong version", mutate: func(e *cacheEnvelope) { e.Version++ }},
		{name: "wrong schema", mutate: func(e *cacheEnvelope) { e.SchemaVersion = "old" }},
		{name: "wrong segment", mutate: func(e *cacheEnvelope) { e.Telemetry.Segment++ }},
		{name: "malformed", raw: "{"},
		{name: "trailing JSON", raw: `{} {}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := t.TempDir()
			qlog := writeCacheQlog(t, dataDir, "qlog")
			parser := &cacheParser{result: testTelemetry(1)}
			cache := NewCache(dataDir, parser)
			path := cachePath(dataDir, "dongle", "route", 1)
			info, err := os.Stat(qlog)
			if err != nil {
				t.Fatal(err)
			}
			envelope := cacheEnvelope{
				Version: CacheVersion, SchemaVersion: cereal.SchemaVersion,
				Source:    sourceFingerprint{Size: info.Size(), ModTimeUnixNano: info.ModTime().UnixNano()},
				Telemetry: testTelemetry(1),
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			var contents []byte
			if tt.raw != "" {
				contents = []byte(tt.raw)
			} else {
				tt.mutate(&envelope)
				contents, err = json.Marshal(envelope)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.Load("dongle", "route", 1, qlog); err != nil {
				t.Fatal(err)
			}
			if parser.callCount() != 1 {
				t.Fatalf("parser calls = %d, want 1", parser.callCount())
			}
		})
	}
}

func TestCacheRebuildsOversizedFile(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "qlog")
	parser := &cacheParser{result: testTelemetry(0)}
	cache := NewCache(dataDir, parser)
	cache.maxBytes = 32
	path := cachePath(dataDir, "dongle", "route", 0)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, 33), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := cache.Load("dongle", "route", 0, qlog); err != nil {
		t.Fatal(err)
	}
	if parser.callCount() != 1 {
		t.Fatalf("parser calls = %d, want 1", parser.callCount())
	}
}

func TestCacheConcurrentSameFingerprintParsesOnceAndClonesSlices(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "qlog")
	parser := &cacheParser{
		result: testTelemetry(0), started: make(chan struct{}, 1), release: make(chan struct{}),
	}
	cache := NewCache(dataDir, parser)
	results := make(chan SegmentTelemetry, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			result, err := cache.Load("dongle", "route", 0, qlog)
			results <- result
			errs <- err
		}()
	}
	<-parser.started
	time.Sleep(20 * time.Millisecond)
	close(parser.release)
	first, second := <-results, <-results
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	first.Speeds[0].Value = 99
	first.GPS[0].Latitude = 99
	first.Controls[0].State = "changed"
	if second.Speeds[0].Value == 99 || second.GPS[0].Latitude == 99 || second.Controls[0].State == "changed" {
		t.Fatalf("waiters share slice backing arrays: %+v / %+v", first, second)
	}
	if parser.callCount() != 1 {
		t.Fatalf("parser calls = %d, want 1", parser.callCount())
	}
}

func TestCacheConcurrentParserErrorIsPublishedToRegisteredWaiter(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "qlog")
	parseErr := errors.New("parse failed")
	parser := &cacheParser{
		err: parseErr, started: make(chan struct{}, 1), release: make(chan struct{}),
	}
	cache := NewCache(dataDir, parser)
	leaderRegistered := make(chan struct{}, 1)
	waiterRegistered := make(chan struct{}, 1)
	cache.afterInflightRegistered = func(leader bool) {
		if leader {
			leaderRegistered <- struct{}{}
		} else {
			waiterRegistered <- struct{}{}
		}
	}

	errs := make(chan error, 2)
	go func() {
		_, err := cache.Load("dongle", "route", 0, qlog)
		errs <- err
	}()
	<-leaderRegistered
	<-parser.started
	go func() {
		_, err := cache.Load("dongle", "route", 0, qlog)
		errs <- err
	}()
	<-waiterRegistered
	close(parser.release)
	for range 2 {
		if err := <-errs; !errors.Is(err, parseErr) {
			t.Fatalf("error = %v, want shared parser error", err)
		}
	}
	if parser.callCount() != 1 {
		t.Fatalf("parser calls = %d, want 1", parser.callCount())
	}
}

func TestCacheConcurrentWriteErrorIsPublishedToRegisteredWaiter(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "qlog")
	parser := &cacheParser{
		result: testTelemetry(0), started: make(chan struct{}, 1), release: make(chan struct{}),
	}
	writeErr := errors.New("write failed")
	cache := NewCache(dataDir, parser)
	cache.syncDir = func(*os.Root) error { return writeErr }
	leaderRegistered := make(chan struct{}, 1)
	waiterRegistered := make(chan struct{}, 1)
	cache.afterInflightRegistered = func(leader bool) {
		if leader {
			leaderRegistered <- struct{}{}
		} else {
			waiterRegistered <- struct{}{}
		}
	}

	errs := make(chan error, 2)
	go func() {
		_, err := cache.Load("dongle", "route", 0, qlog)
		errs <- err
	}()
	<-leaderRegistered
	<-parser.started
	go func() {
		_, err := cache.Load("dongle", "route", 0, qlog)
		errs <- err
	}()
	<-waiterRegistered
	close(parser.release)
	for range 2 {
		if err := <-errs; !errors.Is(err, writeErr) {
			t.Fatalf("error = %v, want shared write error", err)
		}
	}
	if parser.callCount() != 1 {
		t.Fatalf("parser calls = %d, want 1", parser.callCount())
	}
}

func TestCacheDifferentKeysParseConcurrently(t *testing.T) {
	dataDir := t.TempDir()
	firstQlog := writeCacheQlog(t, dataDir, "first")
	secondQlog := writeCacheQlog(t, dataDir, "second")
	parser := &cacheParser{
		result: testTelemetry(0), started: make(chan struct{}, 2), release: make(chan struct{}),
	}
	cache := NewCache(dataDir, parser)
	errs := make(chan error, 2)
	go func() { _, err := cache.Load("dongle", "route", 0, firstQlog); errs <- err }()
	go func() { _, err := cache.Load("dongle", "route", 1, secondQlog); errs <- err }()
	<-parser.started
	select {
	case <-parser.started:
	case <-time.After(time.Second):
		t.Fatal("different cache keys did not parse concurrently")
	}
	close(parser.release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestCacheDifferentFingerprintsSerializeWritesAndRejectStaleResult(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "old")
	cache := NewCache(dataDir, contentTelemetryParser{})
	oldPaused := make(chan struct{})
	resumeOld := make(chan struct{})
	var hookMu sync.Mutex
	first := true
	cache.beforeWriteLock = func(string) {
		hookMu.Lock()
		pause := first
		first = false
		hookMu.Unlock()
		if pause {
			close(oldPaused)
			<-resumeOld
		}
	}
	writeQueued := make(chan struct{}, 2)
	newWriteAcquired := make(chan struct{})
	releaseNewWrite := make(chan struct{})
	var acquireMu sync.Mutex
	firstAcquire := true
	cache.afterWriteLockQueued = func(string) {
		writeQueued <- struct{}{}
	}
	cache.afterWriteLockAcquired = func(string) {
		acquireMu.Lock()
		pause := firstAcquire
		firstAcquire = false
		acquireMu.Unlock()
		if pause {
			close(newWriteAcquired)
			<-releaseNewWrite
		}
	}

	oldErr := make(chan error, 1)
	go func() {
		_, err := cache.Load("dongle", "route", 0, qlog)
		oldErr <- err
	}()
	<-oldPaused
	if err := os.WriteFile(qlog, []byte("new-source"), 0o600); err != nil {
		t.Fatal(err)
	}
	type loadResult struct {
		telemetry SegmentTelemetry
		err       error
	}
	newDone := make(chan loadResult, 1)
	go func() {
		telemetry, err := cache.Load("dongle", "route", 0, qlog)
		newDone <- loadResult{telemetry: telemetry, err: err}
	}()
	<-writeQueued
	<-newWriteAcquired
	close(resumeOld)
	<-writeQueued
	close(releaseNewWrite)
	newResult := <-newDone
	if newResult.err != nil {
		t.Fatal(newResult.err)
	}
	if err := <-oldErr; !errors.Is(err, ErrTelemetrySourceChanged) {
		t.Fatalf("old fingerprint error = %v, want ErrTelemetrySourceChanged", err)
	}
	if newResult.telemetry.Duration != float64(len("new-source")) {
		t.Fatalf("new telemetry = %+v", newResult.telemetry)
	}
	finalPath := cachePath(dataDir, "dongle", "route", 0)
	envelope := readCacheEnvelope(t, finalPath)
	if envelope.Source.Size != int64(len("new-source")) ||
		envelope.Telemetry.Duration != float64(len("new-source")) {
		t.Fatalf("final cache was overwritten by stale result: %+v", envelope)
	}
	assertNoCacheArtifacts(t, filepath.Dir(finalPath))
	cache.writeLocksMu.Lock()
	defer cache.writeLocksMu.Unlock()
	if len(cache.writeLocks) != 0 {
		t.Fatalf("write lock map retained %d entries", len(cache.writeLocks))
	}
}

func TestCacheDifferentFinalPathsWriteConcurrentlyAndCleanLocks(t *testing.T) {
	dataDir := t.TempDir()
	firstQlog := writeCacheQlog(t, dataDir, "first")
	secondQlog := writeCacheQlog(t, dataDir, "second")
	cache := NewCache(dataDir, contentTelemetryParser{})
	acquired := make(chan string, 2)
	release := make(chan struct{})
	cache.afterWriteLockAcquired = func(relPath string) {
		acquired <- relPath
		<-release
	}

	errs := make(chan error, 2)
	go func() {
		_, err := cache.Load("dongle", "route", 0, firstQlog)
		errs <- err
	}()
	go func() {
		_, err := cache.Load("dongle", "route", 1, secondQlog)
		errs <- err
	}()
	paths := make(map[string]bool)
	for range 2 {
		select {
		case relPath := <-acquired:
			paths[relPath] = true
		case <-time.After(time.Second):
			t.Fatal("different final paths did not acquire write locks concurrently")
		}
	}
	if len(paths) != 2 {
		t.Fatalf("acquired write paths = %v, want two", paths)
	}
	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	cache.writeLocksMu.Lock()
	defer cache.writeLocksMu.Unlock()
	if len(cache.writeLocks) != 0 {
		t.Fatalf("write lock map retained %d entries", len(cache.writeLocks))
	}
}

func TestCacheSharesParserErrorButRetries(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "qlog")
	parseErr := errors.New("parse failed")
	parser := &cacheParser{
		err: parseErr, started: make(chan struct{}, 1), release: make(chan struct{}),
	}
	cache := NewCache(dataDir, parser)
	errs := make(chan error, 2)
	for range 2 {
		go func() { _, err := cache.Load("dongle", "route", 0, qlog); errs <- err }()
	}
	<-parser.started
	time.Sleep(20 * time.Millisecond)
	close(parser.release)
	for range 2 {
		if err := <-errs; !errors.Is(err, parseErr) {
			t.Fatalf("error = %v, want parser error", err)
		}
	}
	parser.release = nil
	if _, err := cache.Load("dongle", "route", 0, qlog); !errors.Is(err, parseErr) {
		t.Fatalf("retry error = %v", err)
	}
	if parser.callCount() != 2 {
		t.Fatalf("parser calls = %d, want 2", parser.callCount())
	}
}

func TestCacheRejectsChangedSourceWithoutWriting(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "old")
	parser := &cacheParser{result: testTelemetry(0)}
	parser.onParse = func() {
		if err := os.WriteFile(qlog, []byte("changed"), 0o600); err != nil {
			panic(err)
		}
	}
	cache := NewCache(dataDir, parser)

	_, err := cache.Load("dongle", "route", 0, qlog)
	if !errors.Is(err, ErrTelemetrySourceChanged) {
		t.Fatalf("error = %v, want ErrTelemetrySourceChanged", err)
	}
	if _, err := os.Stat(cachePath(dataDir, "dongle", "route", 0)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache stat error = %v, want not exist", err)
	}
}

func TestCacheRejectsFIFOReplacementWithoutBlocking(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "qlog")
	parser := &cacheParser{result: testTelemetry(0)}
	cache := NewCache(dataDir, parser)
	cache.beforeSourceOpen = func() {
		if err := os.Remove(qlog); err != nil {
			panic(err)
		}
		if err := syscall.Mkfifo(qlog, 0o600); err != nil {
			panic(err)
		}
	}

	done := make(chan error, 1)
	go func() {
		_, err := cache.Load("dongle", "route", 0, qlog)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrTelemetrySourceChanged) {
			t.Fatalf("error = %v, want ErrTelemetrySourceChanged", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Load blocked opening replacement FIFO")
	}
	if parser.callCount() != 0 {
		t.Fatalf("parser calls = %d, want 0", parser.callCount())
	}
}

func TestCacheRejectsSameFingerprintDifferentInode(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "qlog")
	info, err := os.Stat(qlog)
	if err != nil {
		t.Fatal(err)
	}
	parser := &cacheParser{result: testTelemetry(0)}
	cache := NewCache(dataDir, parser)
	cache.beforeSourceOpen = func() {
		replacement := qlog + ".replacement"
		if err := os.WriteFile(replacement, []byte("qlog"), 0o600); err != nil {
			panic(err)
		}
		if err := os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
			panic(err)
		}
		if err := os.Rename(replacement, qlog); err != nil {
			panic(err)
		}
	}

	if _, err := cache.Load("dongle", "route", 0, qlog); !errors.Is(err, ErrTelemetrySourceChanged) {
		t.Fatalf("error = %v, want ErrTelemetrySourceChanged", err)
	}
	if parser.callCount() != 0 {
		t.Fatalf("parser calls = %d, want 0", parser.callCount())
	}
}

func TestCacheInvalidatesPersistedSameFingerprintDifferentInode(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "qlog")
	parser := &cacheParser{result: testTelemetry(0)}
	cache := NewCache(dataDir, parser)
	if _, err := cache.Load("dongle", "route", 0, qlog); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(qlog)
	if err != nil {
		t.Fatal(err)
	}
	replacement := qlog + ".replacement"
	if err := os.WriteFile(replacement, []byte("qlog"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, qlog); err != nil {
		t.Fatal(err)
	}

	if _, err := cache.Load("dongle", "route", 0, qlog); err != nil {
		t.Fatal(err)
	}
	if parser.callCount() != 2 {
		t.Fatalf("parser calls = %d, want 2 after inode replacement", parser.callCount())
	}
}

func TestCacheLoadFileRejectsSafeSourceReplacement(t *testing.T) {
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "uploads", "dongle", "route", "0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "qlog.zst")
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := NewLocator(dataDir).OpenQlog("dongle", "route", Segment{
		Number: 0, QlogRelPath: "route/0/qlog.zst",
	})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "qlog.zst")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	parser := &cacheParser{result: testTelemetry(0)}
	cache := NewCache(dataDir, parser)
	cache.beforeSourceOpen = func() {
		cache.beforeSourceOpen = nil
		if err := os.Remove(target); err != nil {
			panic(err)
		}
		if err := os.Symlink(outside, target); err != nil {
			panic(err)
		}
	}

	if _, err := cache.LoadFile("dongle", "route", 0, source); !errors.Is(err, ErrTelemetrySourceChanged) {
		t.Fatalf("error = %v, want ErrTelemetrySourceChanged", err)
	}
	if parser.callCount() != 0 {
		t.Fatalf("parser calls = %d, want 0", parser.callCount())
	}
}

func TestCacheAtomicWriteLeavesFinalAndNoTemp(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "qlog")
	cache := NewCache(dataDir, &cacheParser{result: testTelemetry(0)})
	if _, err := cache.Load("dongle", "route", 0, qlog); err != nil {
		t.Fatal(err)
	}
	path := cachePath(dataDir, "dongle", "route", 0)
	readCacheEnvelope(t, path)
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("cache directory entries = %+v", entries)
	}
}

func TestCacheRecoversFixedBackupWithoutFinalPathGap(t *testing.T) {
	tests := []struct {
		name      string
		final     []byte
		backup    []byte
		wantFinal []byte
	}{
		{name: "only backup", backup: []byte("old"), wantFinal: []byte("old")},
		{name: "final and stale backup", final: []byte("current"), backup: []byte("stale"), wantFinal: []byte("current")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataDir := t.TempDir()
			relPath := path.Join("replay-cache", "dongle", "route", "0.v1.json")
			finalPath := filepath.Join(dataDir, filepath.FromSlash(relPath))
			backupPath := finalPath + ".backup"
			if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if test.final != nil {
				if err := os.WriteFile(finalPath, test.final, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(backupPath, test.backup, 0o600); err != nil {
				t.Fatal(err)
			}
			stopErr := errors.New("stop before final replace")
			cache := NewCache(dataDir, &cacheParser{})
			cache.beforeFinalReplace = func(root *os.Root, final, backup string) error {
				finalInfo, err := root.Stat(final)
				if err != nil {
					t.Fatalf("final missing before replace: %v", err)
				}
				backupInfo, err := root.Stat(backup)
				if err != nil {
					t.Fatalf("backup missing before replace: %v", err)
				}
				if !os.SameFile(finalInfo, backupInfo) {
					t.Fatal("backup is not a hard link to the live final")
				}
				return stopErr
			}

			err := cache.writeCacheAtomically(relPath, cacheEnvelope{
				Version: CacheVersion, SchemaVersion: cereal.SchemaVersion,
				Telemetry: testTelemetry(0),
			})
			if !errors.Is(err, stopErr) {
				t.Fatalf("error = %v, want injected stop", err)
			}
			got, err := os.ReadFile(finalPath)
			if err != nil {
				t.Fatalf("formal cache path disappeared: %v", err)
			}
			if !bytes.Equal(got, test.wantFinal) {
				t.Fatalf("final = %q, want %q", got, test.wantFinal)
			}
			if _, err := os.Stat(backupPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("backup stat error = %v, want not exist", err)
			}
		})
	}
}

func TestCacheBackupCleanupFailureKeepsDurableNewFinal(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "old")
	parser := &cacheParser{result: testTelemetry(0)}
	cache := NewCache(dataDir, parser)
	if _, err := cache.Load("dongle", "route", 0, qlog); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(qlog, []byte("new source"), 0o600); err != nil {
		t.Fatal(err)
	}
	parser.result.Duration = 99
	cleanupErr := errors.New("injected backup cleanup failure")
	cache.removeBackup = func(*os.Root, string) error { return cleanupErr }

	telemetry, err := cache.Load("dongle", "route", 0, qlog)
	if err != nil {
		t.Fatalf("durable install reported cleanup failure: %v", err)
	}
	if telemetry.Duration != 99 {
		t.Fatalf("telemetry = %+v", telemetry)
	}
	finalPath := cachePath(dataDir, "dongle", "route", 0)
	if got := readCacheEnvelope(t, finalPath).Telemetry.Duration; got != 99 {
		t.Fatalf("final duration = %v, want 99", got)
	}
	if _, err := os.Stat(finalPath + ".backup"); err != nil {
		t.Fatalf("stale backup missing: %v", err)
	}
}

func TestCacheFailedAtomicWritePreservesPreviousCache(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "old")
	parser := &cacheParser{result: testTelemetry(0)}
	cache := NewCache(dataDir, parser)
	if _, err := cache.Load("dongle", "route", 0, qlog); err != nil {
		t.Fatal(err)
	}
	path := cachePath(dataDir, "dongle", "route", 0)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := os.WriteFile(qlog, []byte("new source"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := cache.Load("dongle", "route", 0, qlog); err == nil {
		t.Skip("filesystem permits writes to a read-only directory")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("failed cache write changed previous cache")
	}
}

func TestCachePostRenameSyncFailureRestoresPreviousCache(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "old")
	parser := &cacheParser{result: testTelemetry(0)}
	cache := NewCache(dataDir, parser)
	if _, err := cache.Load("dongle", "route", 0, qlog); err != nil {
		t.Fatal(err)
	}
	path := cachePath(dataDir, "dongle", "route", 0)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(qlog, []byte("new source"), 0o600); err != nil {
		t.Fatal(err)
	}
	parser.result.Duration = 99
	cache.syncDir = func(*os.Root) error { return errors.New("injected parent sync failure") }

	if _, err := cache.Load("dongle", "route", 0, qlog); err == nil {
		t.Fatal("Load succeeded despite injected parent sync failure")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("post-rename sync failure did not restore previous cache")
	}
	assertNoCacheArtifacts(t, filepath.Dir(path))
}

func TestCacheCleanupSyncFailureKeepsDurableNewCache(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "old")
	parser := &cacheParser{result: testTelemetry(0)}
	cache := NewCache(dataDir, parser)
	if _, err := cache.Load("dongle", "route", 0, qlog); err != nil {
		t.Fatal(err)
	}
	path := cachePath(dataDir, "dongle", "route", 0)
	if err := os.WriteFile(qlog, []byte("new source"), 0o600); err != nil {
		t.Fatal(err)
	}
	parser.result.Duration = 99
	syncCalls := 0
	cache.syncDir = func(root *os.Root) error {
		syncCalls++
		if syncCalls == 3 {
			return errors.New("injected cleanup sync failure")
		}
		return syncCacheDir(root)
	}

	telemetry, err := cache.Load("dongle", "route", 0, qlog)
	if err != nil {
		t.Fatalf("durable install reported cleanup sync failure: %v", err)
	}
	if telemetry.Duration != 99 || readCacheEnvelope(t, path).Telemetry.Duration != 99 {
		t.Fatalf("new telemetry was not retained: %+v", telemetry)
	}
	assertNoCacheArtifacts(t, filepath.Dir(path))
}

func TestCachePostRenameSyncFailureRemovesNewCache(t *testing.T) {
	dataDir := t.TempDir()
	qlog := writeCacheQlog(t, dataDir, "qlog")
	cache := NewCache(dataDir, &cacheParser{result: testTelemetry(0)})
	cache.syncDir = func(*os.Root) error { return errors.New("injected parent sync failure") }

	if _, err := cache.Load("dongle", "route", 0, qlog); err == nil {
		t.Fatal("Load succeeded despite injected parent sync failure")
	}
	path := cachePath(dataDir, "dongle", "route", 0)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final cache stat error = %v, want not exist", err)
	}
	assertNoCacheArtifacts(t, filepath.Dir(path))
}

func TestCacheRejectsSymlinkedParentsAndTightensPermissions(t *testing.T) {
	for _, parent := range []string{
		"replay-cache",
		filepath.Join("replay-cache", "dongle"),
		filepath.Join("replay-cache", "dongle", "route"),
	} {
		t.Run("symlink "+parent, func(t *testing.T) {
			dataDir := t.TempDir()
			outside := t.TempDir()
			link := filepath.Join(dataDir, parent)
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			qlog := writeCacheQlog(t, dataDir, "qlog")
			cache := NewCache(dataDir, &cacheParser{result: testTelemetry(0)})
			if _, err := cache.Load("dongle", "route", 0, qlog); err == nil {
				t.Fatal("Load traversed symlinked cache parent")
			}
			entries, err := os.ReadDir(outside)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("outside directory modified: %+v", entries)
			}
		})
	}

	t.Run("existing permissions", func(t *testing.T) {
		dataDir := t.TempDir()
		routeDir := filepath.Join(dataDir, "replay-cache", "dongle", "route")
		if err := os.MkdirAll(routeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, dir := range []string{
			filepath.Join(dataDir, "replay-cache"),
			filepath.Join(dataDir, "replay-cache", "dongle"),
			routeDir,
		} {
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		qlog := writeCacheQlog(t, dataDir, "qlog")
		cache := NewCache(dataDir, &cacheParser{result: testTelemetry(0)})
		if _, err := cache.Load("dongle", "route", 0, qlog); err != nil {
			t.Fatal(err)
		}
		for _, dir := range []string{
			filepath.Join(dataDir, "replay-cache"),
			filepath.Join(dataDir, "replay-cache", "dongle"),
			routeDir,
		} {
			info, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o700 {
				t.Fatalf("%s permissions = %o, want 700", dir, info.Mode().Perm())
			}
		}
	})
}

func TestCacheRejectsInvalidInputs(t *testing.T) {
	qlog := writeCacheQlog(t, t.TempDir(), "qlog")
	cache := NewCache(t.TempDir(), &cacheParser{result: testTelemetry(0)})
	for _, test := range []struct {
		dongle, route string
		segment       int
	}{
		{dongle: "../dongle", route: "route"},
		{dongle: "dongle", route: "route/child"},
		{dongle: "dongle", route: "route", segment: -1},
	} {
		if _, err := cache.Load(test.dongle, test.route, test.segment, qlog); err == nil {
			t.Fatalf("Load(%q, %q, %d) succeeded", test.dongle, test.route, test.segment)
		}
	}
}

func writeCacheQlog(t *testing.T, dataDir, contents string) string {
	t.Helper()
	path := filepath.Join(dataDir, contents+".zst")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testTelemetry(segment int) SegmentTelemetry {
	return SegmentTelemetry{
		Segment: segment, Duration: 12.5,
		Speeds:   []SpeedSample{{Time: 1, Value: 2}},
		GPS:      []GPSSample{{Time: 1, Latitude: 2}},
		Controls: []ControlSample{{Time: 1, State: "enabled"}},
	}
}

func cachePath(dataDir, dongleID, route string, segment int) string {
	return filepath.Join(dataDir, "replay-cache", dongleID, route,
		fmt.Sprintf("%d.v%d.json", segment, CacheVersion))
}

func readCacheEnvelope(t *testing.T, path string) cacheEnvelope {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var envelope cacheEnvelope
	if err := json.NewDecoder(file).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	return envelope
}

func assertNoCacheArtifacts(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") || strings.Contains(entry.Name(), ".backup") {
			t.Fatalf("cache artifact left behind: %s", entry.Name())
		}
	}
}
