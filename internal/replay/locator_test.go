package replay

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"testing"
)

func TestLocatorFindsQCameraAndQlog(t *testing.T) {
	dataDir := t.TempDir()
	routeDir := filepath.Join(dataDir, "uploads", "dongle", "route", "0")
	if err := os.MkdirAll(routeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	qcamera := filepath.Join(routeDir, "qcamera.ts")
	qlog := filepath.Join(routeDir, "qlog.zst")
	if err := os.WriteFile(qcamera, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(qlog, []byte("log"), 0o644); err != nil {
		t.Fatal(err)
	}

	locator := NewLocator(dataDir)
	segment := Segment{
		Number:         0,
		QCameraRelPath: "route/0/qcamera.ts",
		QlogRelPath:    "route/0/qlog.zst",
	}

	gotQCamera, err := locator.OpenQCamera("dongle", "route", segment)
	if err != nil {
		t.Fatalf("OpenQCamera() error = %v", err)
	}
	defer gotQCamera.Close()
	if gotQCamera.Name() != qcamera {
		t.Fatalf("OpenQCamera().Name() = %q, want %q", gotQCamera.Name(), qcamera)
	}

	gotQlog, err := locator.OpenQlog("dongle", "route", segment)
	if err != nil {
		t.Fatalf("OpenQlog() error = %v", err)
	}
	defer gotQlog.Close()
	if gotQlog.Name() != qlog {
		t.Fatalf("OpenQlog().Name() = %q, want %q", gotQlog.Name(), qlog)
	}
}

func TestLocatorFindsTwoLevelDragonPilotFiles(t *testing.T) {
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "uploads", "dongle", "route--with--parts--12")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{"qcamera.ts": "video", "qlog.zst": "log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	segment := Segment{
		Number:         12,
		QCameraRelPath: "route--with--parts--12/qcamera.ts",
		QlogRelPath:    "route--with--parts--12/qlog.zst",
	}
	locator := NewLocator(dataDir)

	qcamera, err := locator.OpenQCamera("dongle", "route--with--parts", segment)
	if err != nil {
		t.Fatal(err)
	}
	defer qcamera.Close()
	qlog, err := locator.OpenQlog("dongle", "route--with--parts", segment)
	if err != nil {
		t.Fatal(err)
	}
	defer qlog.Close()
	if qcamera.Name() != filepath.Join(dir, "qcamera.ts") ||
		qlog.Name() != filepath.Join(dir, "qlog.zst") {
		t.Fatalf("located paths = %q, %q", qcamera.Name(), qlog.Name())
	}
}

func TestLocatorRejectsTwoLevelRouteScopeBypass(t *testing.T) {
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "uploads", "dongle", "other--12")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "qcamera.ts"), []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewLocator(dataDir).OpenQCamera("dongle", "route", Segment{
		Number: 12, QCameraRelPath: "other--12/qcamera.ts",
	})
	if err == nil {
		t.Fatal("OpenQCamera accepted two-level path outside route scope")
	}
}

func TestLocatorRejectsTwoLevelReplacementAfterValidation(t *testing.T) {
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "uploads", "dongle", "route--0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "qcamera.ts")
	if err := os.WriteFile(target, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "qcamera.ts")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	locator := NewLocator(dataDir)
	locator.beforeFileOpen = func() {
		if err := os.Remove(target); err != nil {
			panic(err)
		}
		if err := os.Symlink(outside, target); err != nil {
			panic(err)
		}
	}
	file, err := locator.OpenQCamera("dongle", "route", Segment{
		Number: 0, QCameraRelPath: "route--0/qcamera.ts",
	})
	if file != nil {
		defer file.Close()
		data, readErr := io.ReadAll(file)
		if readErr == nil && string(data) == "outside" {
			t.Fatal("OpenQCamera read replacement symlink target")
		}
	}
	if err == nil {
		t.Fatal("OpenQCamera accepted replacement symlink")
	}
}

func TestLocatorReportsMissingFile(t *testing.T) {
	locator := NewLocator(t.TempDir())
	_, err := locator.OpenQCamera("dongle", "route", Segment{
		QCameraRelPath: "route/0/qcamera.ts",
	})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("QCameraPath() error = %v, want fs.ErrNotExist", err)
	}
}

func TestLocatorRejectsDirectory(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(dataDir, "uploads", "dongle", "route", "0", "qcamera.ts")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	locator := NewLocator(dataDir)
	_, err := locator.OpenQCamera("dongle", "route", Segment{
		QCameraRelPath: "route/0/qcamera.ts",
	})
	if err == nil {
		t.Fatal("QCameraPath() error = nil, want validation error")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("QCameraPath() error = %v, want non-missing validation error", err)
	}
}

func TestLocatorRejectsWrongMediaType(t *testing.T) {
	dataDir := t.TempDir()
	routeDir := filepath.Join(dataDir, "uploads", "dongle", "route", "0")
	if err := os.MkdirAll(routeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"qcamera.ts", "qlog.zst"} {
		if err := os.WriteFile(filepath.Join(routeDir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	locator := NewLocator(dataDir)
	if _, err := locator.OpenQlog("dongle", "route", Segment{
		QlogRelPath: "route/0/qcamera.ts",
	}); err == nil {
		t.Fatal("QlogPath() error = nil, want media type validation error")
	}
	if _, err := locator.OpenQCamera("dongle", "route", Segment{
		QCameraRelPath: "route/0/qlog.zst",
	}); err == nil {
		t.Fatal("QCameraPath() error = nil, want media type validation error")
	}
}

func TestLocatorRejectsInvalidDeviceOrRoute(t *testing.T) {
	locator := NewLocator(t.TempDir())
	segment := Segment{QCameraRelPath: "route/0/qcamera.ts"}
	tests := []struct {
		name     string
		dongleID string
		route    string
	}{
		{name: "empty dongle", dongleID: "", route: "route"},
		{name: "dongle slash", dongleID: "a/b", route: "route"},
		{name: "dongle backslash", dongleID: `a\b`, route: "route"},
		{name: "dongle dot", dongleID: ".", route: "route"},
		{name: "dongle parent", dongleID: "..", route: "route"},
		{name: "empty route", dongleID: "dongle", route: ""},
		{name: "route slash", dongleID: "dongle", route: "a/b"},
		{name: "route backslash", dongleID: "dongle", route: `a\b`},
		{name: "route dot", dongleID: "dongle", route: "."},
		{name: "route parent", dongleID: "dongle", route: ".."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := locator.OpenQCamera(tt.dongleID, tt.route, segment)
			if err == nil {
				t.Fatal("QCameraPath() error = nil, want validation error")
			}
			if errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("QCameraPath() error = %v, want input validation error", err)
			}
		})
	}
}

func TestLocatorRejectsEscapingSymlink(t *testing.T) {
	dataDir := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "qcamera.ts")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	targetDir := filepath.Join(dataDir, "uploads", "dongle", "route", "0")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(targetDir, "qcamera.ts")); err != nil {
		t.Fatal(err)
	}

	locator := NewLocator(dataDir)
	_, err := locator.OpenQCamera("dongle", "route", Segment{
		QCameraRelPath: "route/0/qcamera.ts",
	})
	if err == nil {
		t.Fatal("QCameraPath() error = nil, want symlink escape error")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("QCameraPath() error = %v, want validation error", err)
	}
}

func TestLocatorOpenQCameraDoesNotFollowReplacementAfterValidation(t *testing.T) {
	dataDir := t.TempDir()
	routeDir := filepath.Join(dataDir, "uploads", "dongle", "route", "0")
	if err := os.MkdirAll(routeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(routeDir, "qcamera.ts")
	if err := os.WriteFile(target, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "qcamera.ts")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}

	locator := NewLocator(dataDir)
	locator.beforeFileOpen = func() {
		if err := os.Remove(target); err != nil {
			panic(err)
		}
		if err := os.Symlink(outside, target); err != nil {
			panic(err)
		}
	}
	file, err := locator.OpenQCamera("dongle", "route", Segment{
		Number: 0, QCameraRelPath: "route/0/qcamera.ts",
	})
	if file != nil {
		defer file.Close()
		data, readErr := io.ReadAll(file)
		if readErr == nil && string(data) == "outside" {
			t.Fatal("OpenQCamera read replacement symlink target")
		}
	}
	if err == nil {
		t.Fatal("OpenQCamera() error = nil, want replacement rejection")
	}
}

func TestLocatorRejectsSymlinkedMediaComponents(t *testing.T) {
	for _, component := range []string{"device", "route", "segment"} {
		t.Run(component, func(t *testing.T) {
			dataDir := t.TempDir()
			uploads := filepath.Join(dataDir, "uploads")
			realRoute := filepath.Join(uploads, "dongle", "other-route", "0")
			if err := os.MkdirAll(realRoute, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(realRoute, "qcamera.ts"), []byte("other"), 0o644); err != nil {
				t.Fatal(err)
			}
			switch component {
			case "device":
				if err := os.RemoveAll(filepath.Join(uploads, "dongle")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(t.TempDir()), filepath.Join(uploads, "dongle")); err != nil {
					t.Fatal(err)
				}
			case "route":
				if err := os.Symlink("other-route", filepath.Join(uploads, "dongle", "route")); err != nil {
					t.Fatal(err)
				}
			case "segment":
				routeDir := filepath.Join(uploads, "dongle", "route")
				if err := os.MkdirAll(routeDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join("..", "other-route", "0"), filepath.Join(routeDir, "0")); err != nil {
					t.Fatal(err)
				}
			}

			file, err := NewLocator(dataDir).OpenQCamera("dongle", "route", Segment{
				Number: 0, QCameraRelPath: "route/0/qcamera.ts",
			})
			if file != nil {
				file.Close()
			}
			if err == nil {
				t.Fatalf("OpenQCamera traversed symlinked %s component", component)
			}
		})
	}
}

func TestLocatorRepeatedOpenDoesNotLeakFileDescriptors(t *testing.T) {
	oldGCPercent := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(oldGCPercent)
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	maxFD := int(min(limit.Cur, 65_536))
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "uploads", "dongle", "route", "0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "qcamera.ts"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	locator := NewLocator(dataDir)
	segment := Segment{Number: 0, QCameraRelPath: "route/0/qcamera.ts"}
	before := countOpenFDs(t, maxFD)

	for range 256 {
		file, err := locator.OpenQCamera("dongle", "route", segment)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	missing := Segment{Number: 0, QCameraRelPath: "route/0/qcamera.ts"}
	if err := os.Remove(filepath.Join(dir, "qcamera.ts")); err != nil {
		t.Fatal(err)
	}
	for range 256 {
		file, err := locator.OpenQCamera("dongle", "route", missing)
		if file != nil {
			file.Close()
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing open error = %v", err)
		}
	}

	after := countOpenFDs(t, maxFD)
	if after > before+4 {
		t.Fatalf("open file descriptors grew from %d to %d", before, after)
	}
}

func countOpenFDs(t *testing.T, maxFD int) int {
	t.Helper()
	count := 0
	for fd := 0; fd < maxFD; fd++ {
		_, _, errno := syscall.Syscall(
			syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_GETFD), 0,
		)
		if errno != syscall.EBADF {
			count++
		}
	}
	return count
}
