package replay

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"pilotserver/internal/routepath"
)

type Locator struct {
	dataDir        string
	beforeFileOpen func()
}

type LocatedFile struct {
	*os.File
	info   os.FileInfo
	reopen func() (*LocatedFile, error)
}

func NewLocator(dataDir string) *Locator {
	return &Locator{dataDir: dataDir}
}

func (l *Locator) OpenQCamera(dongleID, route string, segment Segment) (*LocatedFile, error) {
	return l.openMedia(dongleID, route, segment, segment.QCameraRelPath, "qcamera.ts")
}

func (l *Locator) OpenQlog(dongleID, route string, segment Segment) (*LocatedFile, error) {
	return l.openMedia(dongleID, route, segment, segment.QlogRelPath, "qlog.zst")
}

func (l *Locator) openMedia(dongleID, route string, segment Segment, relPath, filename string) (*LocatedFile, error) {
	if err := validatePathComponent("dongle ID", dongleID); err != nil {
		return nil, err
	}
	if err := validatePathComponent("route", route); err != nil {
		return nil, err
	}

	parsed, ok := routepath.ParseSegmentFile(relPath)
	if !ok {
		return nil, fmt.Errorf("invalid segment relative path %q", relPath)
	}
	number, err := parseSegmentNumber(parsed.SegmentName)
	if err != nil {
		return nil, err
	}
	if parsed.RouteName != route || number != segment.Number || parsed.Filename != filename {
		return nil, fmt.Errorf("relative path %q does not match requested media", relPath)
	}

	root, err := os.OpenRoot(l.dataDir)
	if err != nil {
		return nil, fmt.Errorf("open data root: %w", err)
	}
	directories := []string{"uploads", dongleID}
	if strings.Count(relPath, "/") == 1 {
		directories = append(directories, strings.SplitN(relPath, "/", 2)[0])
	} else {
		directories = append(directories, route, parsed.SegmentName)
	}
	for _, component := range directories {
		next, err := openAnchoredDir(root, component)
		if err != nil {
			root.Close()
			return nil, err
		}
		if err := root.Close(); err != nil {
			next.Close()
			return nil, fmt.Errorf("close media directory: %w", err)
		}
		root = next
	}
	defer root.Close()

	before, err := root.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("media path %q is not a regular file", relPath)
	}
	if l.beforeFileOpen != nil {
		l.beforeFileOpen()
	}
	file, err := root.OpenFile(filename, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		file.Close()
		return nil, errors.New("media source changed while opening")
	}
	return &LocatedFile{
		File: file,
		info: opened,
		reopen: func() (*LocatedFile, error) {
			return l.openMedia(dongleID, route, segment, relPath, filename)
		},
	}, nil
}

func openAnchoredDir(parent *os.Root, name string) (*os.Root, error) {
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
		return nil, fmt.Errorf("media path component %q is not a directory", name)
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		child.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("media directory changed while opening")
	}
	return child, nil
}

func validatePathComponent(name, value string) error {
	if value == "" || value == "." || value == ".." ||
		strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("invalid %s %q", name, value)
	}
	return nil
}

func (f *LocatedFile) sourceInfo() os.FileInfo {
	return f.info
}

func (f *LocatedFile) reopenCurrent() (*LocatedFile, error) {
	return f.reopen()
}
