package replay

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sync"
	"syscall"

	"pilotserver/internal/replay/cereal"
)

const (
	CacheVersion         = 2
	defaultMaxCacheBytes = 256 << 20
)

var ErrTelemetrySourceChanged = errors.New("telemetry source changed")

type SegmentParser interface {
	ParseSegment(io.Reader, int) (SegmentTelemetry, error)
}

type Cache struct {
	dataDir  string
	parser   SegmentParser
	maxBytes int64
	syncDir  func(*os.Root) error

	afterInflightRegistered func(bool)
	beforeSourceOpen        func()
	beforeWriteLock         func(string)
	afterWriteLockQueued    func(string)
	afterWriteLockAcquired  func(string)
	beforeFinalReplace      func(*os.Root, string, string) error
	removeBackup            func(*os.Root, string) error

	mu       sync.Mutex
	inflight map[cacheKey]*cacheCall

	writeLocksMu sync.Mutex
	writeLocks   map[string]*cachePathLock
}

type sourceFingerprint struct {
	Size            int64  `json:"size"`
	ModTimeUnixNano int64  `json:"mod_time_unix_nano"`
	Device          uint64 `json:"device"`
	Inode           uint64 `json:"inode"`
}

type cacheEnvelope struct {
	Version       int               `json:"version"`
	SchemaVersion string            `json:"schema_version"`
	Source        sourceFingerprint `json:"source"`
	Telemetry     SegmentTelemetry  `json:"telemetry"`
}

type cacheKey struct {
	path        string
	fingerprint sourceFingerprint
	identity    sourceIdentity
}

type sourceIdentity struct {
	device uint64
	inode  uint64
}

type cacheCall struct {
	done      chan struct{}
	telemetry SegmentTelemetry
	err       error
}

type cachePathLock struct {
	mu   sync.Mutex
	refs int
}

func NewCache(dataDir string, parser SegmentParser) *Cache {
	return &Cache{
		dataDir: dataDir, parser: parser, maxBytes: defaultMaxCacheBytes,
		syncDir:      syncCacheDir,
		removeBackup: func(root *os.Root, name string) error { return root.Remove(name) },
		inflight:     make(map[cacheKey]*cacheCall),
		writeLocks:   make(map[string]*cachePathLock),
	}
}

func (c *Cache) LoadCachedFile(dongleID, route string, segment int, source *LocatedFile) (SegmentTelemetry, bool, error) {
	if source == nil || source.File == nil {
		return SegmentTelemetry{}, false, errors.New("telemetry source is not open")
	}
	defer source.Close()
	if err := validatePathComponent("dongle ID", dongleID); err != nil {
		return SegmentTelemetry{}, false, err
	}
	if err := validatePathComponent("route", route); err != nil {
		return SegmentTelemetry{}, false, err
	}
	if segment < 0 {
		return SegmentTelemetry{}, false, fmt.Errorf("%w: %d", ErrInvalidSegment, segment)
	}
	info := source.sourceInfo()
	if !info.Mode().IsRegular() {
		return SegmentTelemetry{}, false, errors.New("telemetry source is not a regular file")
	}
	telemetry, hit, err := c.read(c.path(dongleID, route, segment), fingerprintOf(info), segment)
	if err != nil || !hit {
		return SegmentTelemetry{}, false, err
	}
	if err := c.ensureSourceCurrent(source, info); err != nil {
		return SegmentTelemetry{}, false, err
	}
	return cloneTelemetry(telemetry), true, nil
}

func (c *Cache) LoadFile(dongleID, route string, segment int, source *LocatedFile) (SegmentTelemetry, error) {
	if source == nil || source.File == nil {
		return SegmentTelemetry{}, errors.New("telemetry source is not open")
	}
	defer source.Close()
	if err := validatePathComponent("dongle ID", dongleID); err != nil {
		return SegmentTelemetry{}, err
	}
	if err := validatePathComponent("route", route); err != nil {
		return SegmentTelemetry{}, err
	}
	if segment < 0 {
		return SegmentTelemetry{}, fmt.Errorf("%w: %d", ErrInvalidSegment, segment)
	}
	if c.parser == nil {
		return SegmentTelemetry{}, errors.New("telemetry parser is not configured")
	}
	info := source.sourceInfo()
	if !info.Mode().IsRegular() {
		return SegmentTelemetry{}, errors.New("telemetry source is not a regular file")
	}
	fingerprint := fingerprintOf(info)
	cachePath := c.path(dongleID, route, segment)
	key := cacheKey{path: cachePath, fingerprint: fingerprint, identity: identityOf(info)}
	c.mu.Lock()
	if call := c.inflight[key]; call != nil {
		c.mu.Unlock()
		if c.afterInflightRegistered != nil {
			c.afterInflightRegistered(false)
		}
		<-call.done
		return cloneTelemetry(call.telemetry), call.err
	}
	call := &cacheCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()
	if c.afterInflightRegistered != nil {
		c.afterInflightRegistered(true)
	}

	telemetry, hit, err := c.read(cachePath, fingerprint, segment)
	if err == nil {
		err = c.ensureSourceCurrent(source, info)
	}
	if err == nil && !hit {
		telemetry, err = c.parseLocatedAndStore(cachePath, segment, source, info)
	}
	c.finishCall(key, call, telemetry, err)
	return cloneTelemetry(telemetry), err
}

func (c *Cache) finishCall(key cacheKey, call *cacheCall, telemetry SegmentTelemetry, err error) {
	c.mu.Lock()
	call.telemetry = telemetry
	call.err = err
	delete(c.inflight, key)
	close(call.done)
	c.mu.Unlock()
}

func (c *Cache) path(dongleID, route string, segment int) string {
	return path.Join("replay-cache", dongleID, route,
		fmt.Sprintf("%d.v%d.json", segment, CacheVersion))
}

func (c *Cache) read(relPath string, source sourceFingerprint, segment int) (SegmentTelemetry, bool, error) {
	root, err := os.OpenRoot(c.dataDir)
	if err != nil {
		return SegmentTelemetry{}, false, fmt.Errorf("open telemetry cache root: %w", err)
	}
	defer root.Close()
	dir := path.Dir(relPath)
	exists, err := prepareCacheDir(root, dir, false)
	if err != nil {
		return SegmentTelemetry{}, false, err
	}
	if !exists {
		return SegmentTelemetry{}, false, nil
	}
	info, err := root.Lstat(relPath)
	if errors.Is(err, os.ErrNotExist) {
		return SegmentTelemetry{}, false, nil
	}
	if err != nil {
		return SegmentTelemetry{}, false, fmt.Errorf("inspect telemetry cache: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return SegmentTelemetry{}, false, errors.New("telemetry cache must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return SegmentTelemetry{}, false, nil
	}
	file, err := root.OpenFile(relPath, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return SegmentTelemetry{}, false, fmt.Errorf("open telemetry cache: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, c.maxBytes+1))
	if err != nil {
		return SegmentTelemetry{}, false, fmt.Errorf("read telemetry cache: %w", err)
	}
	if int64(len(data)) > c.maxBytes {
		return SegmentTelemetry{}, false, nil
	}
	var envelope cacheEnvelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&envelope); err != nil {
		return SegmentTelemetry{}, false, nil
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return SegmentTelemetry{}, false, nil
	}
	if envelope.Version != CacheVersion ||
		envelope.SchemaVersion != cereal.SchemaVersion ||
		envelope.Source != source ||
		envelope.Telemetry.Segment != segment {
		return SegmentTelemetry{}, false, nil
	}
	return envelope.Telemetry, true, nil
}

func (c *Cache) parseLocatedAndStore(cachePath string, segment int, source *LocatedFile, before os.FileInfo) (SegmentTelemetry, error) {
	telemetry, parseErr := c.parser.ParseSegment(source.File, segment)
	afterParse, statErr := source.Stat()
	if statErr != nil || !sameTelemetrySource(before, afterParse) {
		return SegmentTelemetry{}, ErrTelemetrySourceChanged
	}
	if err := c.ensureSourceCurrent(source, before); err != nil {
		return SegmentTelemetry{}, err
	}
	if parseErr != nil {
		return SegmentTelemetry{}, parseErr
	}
	if c.beforeWriteLock != nil {
		c.beforeWriteLock(cachePath)
	}
	unlock := c.lockCachePath(cachePath)
	defer unlock()
	if c.afterWriteLockAcquired != nil {
		c.afterWriteLockAcquired(cachePath)
	}
	if err := c.ensureSourceCurrent(source, before); err != nil {
		return SegmentTelemetry{}, err
	}
	envelope := cacheEnvelope{
		Version: CacheVersion, SchemaVersion: cereal.SchemaVersion,
		Source: fingerprintOf(before), Telemetry: telemetry,
	}
	if err := c.writeCacheAtomically(cachePath, envelope); err != nil {
		return SegmentTelemetry{}, err
	}
	return telemetry, nil
}

func (c *Cache) ensureSourceCurrent(source *LocatedFile, before os.FileInfo) error {
	if c.beforeSourceOpen != nil {
		c.beforeSourceOpen()
	}
	current, err := source.reopenCurrent()
	if err != nil {
		return ErrTelemetrySourceChanged
	}
	defer current.Close()
	if !sameTelemetrySource(before, current.sourceInfo()) {
		return ErrTelemetrySourceChanged
	}
	return nil
}

func (c *Cache) lockCachePath(relPath string) func() {
	c.writeLocksMu.Lock()
	lock := c.writeLocks[relPath]
	if lock == nil {
		lock = &cachePathLock{}
		c.writeLocks[relPath] = lock
	}
	lock.refs++
	c.writeLocksMu.Unlock()
	if c.afterWriteLockQueued != nil {
		c.afterWriteLockQueued(relPath)
	}

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		c.writeLocksMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(c.writeLocks, relPath)
		}
		c.writeLocksMu.Unlock()
	}
}

func (c *Cache) writeCacheAtomically(relPath string, envelope cacheEnvelope) error {
	root, err := os.OpenRoot(c.dataDir)
	if err != nil {
		return fmt.Errorf("open telemetry cache root: %w", err)
	}
	defer root.Close()
	dir := path.Dir(relPath)
	if _, err := prepareCacheDir(root, dir, true); err != nil {
		return err
	}
	parent, err := root.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open telemetry cache directory: %w", err)
	}
	defer parent.Close()
	final := path.Base(relPath)
	backup := final + ".backup"
	if err := c.recoverCacheBackup(parent, final, backup); err != nil {
		return err
	}
	if info, err := parent.Lstat(final); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("telemetry cache must not be a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect existing telemetry cache: %w", err)
	}

	tempName, temp, err := createPrivateCacheFile(parent, ".telemetry-", ".tmp")
	if err != nil {
		return err
	}
	tempPresent := true
	defer func() {
		_ = temp.Close()
		if tempPresent {
			_ = parent.Remove(tempName)
		}
	}()
	if err := json.NewEncoder(temp).Encode(envelope); err != nil {
		return fmt.Errorf("encode telemetry cache: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync telemetry cache temp file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close telemetry cache temp file: %w", err)
	}

	hadOld, err := regularCacheExists(parent, final)
	if err != nil {
		return err
	}
	if hadOld {
		if err := parent.Link(final, backup); err != nil {
			return fmt.Errorf("link telemetry cache backup: %w", err)
		}
		if err := c.syncDir(parent); err != nil {
			_ = parent.Remove(backup)
			_ = syncCacheDir(parent)
			return fmt.Errorf("sync telemetry cache backup: %w", err)
		}
	}
	if c.beforeFinalReplace != nil {
		if err := c.beforeFinalReplace(parent, final, backup); err != nil {
			if hadOld {
				_ = parent.Remove(backup)
				_ = syncCacheDir(parent)
			}
			return err
		}
	}
	if err := parent.Rename(tempName, final); err != nil {
		if hadOld {
			_ = parent.Remove(backup)
			_ = syncCacheDir(parent)
		}
		return fmt.Errorf("replace telemetry cache: %w", err)
	}
	tempPresent = false
	if err := c.syncDir(parent); err != nil {
		rollbackErr := rollbackInstalledCache(parent, final, backup, hadOld)
		return errors.Join(fmt.Errorf("sync telemetry cache directory: %w", err), rollbackErr)
	}
	if hadOld {
		if err := c.removeBackup(parent, backup); err != nil {
			return nil
		}
		_ = c.syncDir(parent)
	}
	return nil
}

func (c *Cache) recoverCacheBackup(root *os.Root, final, backup string) error {
	backupExists, err := regularCacheExists(root, backup)
	if err != nil {
		return err
	}
	if !backupExists {
		return nil
	}
	finalExists, err := regularCacheExists(root, final)
	if err != nil {
		return err
	}
	if finalExists {
		if err := root.Remove(backup); err != nil {
			return fmt.Errorf("remove stale telemetry cache backup: %w", err)
		}
		_ = c.syncDir(root)
		return nil
	}
	if err := root.Rename(backup, final); err != nil {
		return fmt.Errorf("restore telemetry cache backup: %w", err)
	}
	if err := c.syncDir(root); err != nil {
		_ = root.Rename(final, backup)
		_ = syncCacheDir(root)
		return fmt.Errorf("sync restored telemetry cache backup: %w", err)
	}
	return nil
}

func prepareCacheDir(root *os.Root, dir string, create bool) (bool, error) {
	current := ""
	for _, component := range splitPath(dir) {
		current = path.Join(current, component)
		info, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if !create {
				return false, nil
			}
			if err := root.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return false, fmt.Errorf("create telemetry cache directory: %w", err)
			}
			info, err = root.Lstat(current)
		}
		if err != nil {
			return false, fmt.Errorf("inspect telemetry cache directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("telemetry cache directory must not be a symlink")
		}
		if !info.IsDir() {
			return false, errors.New("telemetry cache parent is not a directory")
		}
		if err := root.Chmod(current, 0o700); err != nil {
			return false, fmt.Errorf("protect telemetry cache directory: %w", err)
		}
	}
	return true, nil
}

func splitPath(name string) []string {
	var components []string
	for name != "." && name != "" {
		dir, base := path.Split(name)
		components = append([]string{base}, components...)
		name = path.Clean(dir)
	}
	return components
}

func createPrivateCacheFile(root *os.Root, prefix, suffix string) (string, *os.File, error) {
	for range 10 {
		name, err := randomCacheName(prefix, suffix)
		if err != nil {
			return "", nil, err
		}
		file, err := root.OpenFile(name,
			os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, fmt.Errorf("create telemetry cache temp file: %w", err)
		}
		if err := file.Chmod(0o600); err != nil {
			_ = file.Close()
			_ = root.Remove(name)
			return "", nil, fmt.Errorf("protect telemetry cache temp file: %w", err)
		}
		return name, file, nil
	}
	return "", nil, errors.New("create unique telemetry cache temp file")
}

func randomCacheName(prefix, suffix string) (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate telemetry cache name: %w", err)
	}
	return prefix + hex.EncodeToString(random[:]) + suffix, nil
}

func regularCacheExists(root *os.Root, name string) (bool, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect telemetry cache path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, errors.New("telemetry cache path is not a regular file")
	}
	return true, nil
}

func rollbackInstalledCache(root *os.Root, final, backup string, hadOld bool) error {
	if hadOld {
		if err := root.Rename(backup, final); err != nil {
			return fmt.Errorf("restore telemetry cache backup: %w", err)
		}
	} else if err := root.Remove(final); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove failed telemetry cache: %w", err)
	}
	_ = syncCacheDir(root)
	return nil
}

func syncCacheDir(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func sameTelemetrySource(first, second os.FileInfo) bool {
	return first != nil && second != nil &&
		first.Mode().IsRegular() && second.Mode().IsRegular() &&
		fingerprintOf(first) == fingerprintOf(second) &&
		os.SameFile(first, second)
}

func fingerprintOf(info os.FileInfo) sourceFingerprint {
	identity := identityOf(info)
	return sourceFingerprint{
		Size: info.Size(), ModTimeUnixNano: info.ModTime().UnixNano(),
		Device: identity.device, Inode: identity.inode,
	}
}

func identityOf(info os.FileInfo) sourceIdentity {
	stat, _ := info.Sys().(*syscall.Stat_t)
	if stat == nil {
		return sourceIdentity{}
	}
	return sourceIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}
}

func cloneTelemetry(telemetry SegmentTelemetry) SegmentTelemetry {
	telemetry.Speeds = append([]SpeedSample(nil), telemetry.Speeds...)
	telemetry.GPS = append([]GPSSample(nil), telemetry.GPS...)
	telemetry.Controls = append([]ControlSample(nil), telemetry.Controls...)
	return telemetry
}
