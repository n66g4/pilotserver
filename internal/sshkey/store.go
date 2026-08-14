package sshkey

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
)

type Store struct {
	dir string
}

type Status struct {
	Configured  bool
	Fingerprint string
}

var ErrInvalidKey = errors.New("invalid SSH private key")

var fileMu sync.Mutex

func Open(dataDir string) (*Store, error) {
	fileMu.Lock()
	defer fileMu.Unlock()

	dir := filepath.Join(dataDir, "ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := ensurePermissions(dir); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Status() (Status, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	if err := ensurePermissions(s.dir); err != nil {
		return Status{}, err
	}
	signer, err := s.signer()
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	return Status{Configured: true, Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}, nil
}

func (s *Store) Import(privateKey []byte) (Status, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	if err := ensurePermissions(s.dir); err != nil {
		return Status{}, err
	}
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return Status{}, ErrInvalidKey
	}
	if err := writeFileAtomic(s.privatePath(), privateKey, 0o600); err != nil {
		return Status{}, err
	}
	_ = os.Remove(s.publicPath())
	return Status{Configured: true, Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}, nil
}

func (s *Store) Clear() error {
	fileMu.Lock()
	defer fileMu.Unlock()
	if err := ensurePermissions(s.dir); err != nil {
		return err
	}
	for _, path := range []string{s.privatePath(), s.publicPath()} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Store) Signer() (ssh.Signer, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	if err := ensurePermissions(s.dir); err != nil {
		return nil, err
	}
	return s.signer()
}

func (s *Store) signer() (ssh.Signer, error) {
	privateKey, err := os.ReadFile(s.privatePath())
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(privateKey)
}

func (s *Store) privatePath() string {
	return filepath.Join(s.dir, "id_ed25519")
}

func (s *Store) publicPath() string {
	return filepath.Join(s.dir, "id_ed25519.pub")
}

func ensurePermissions(dir string) error {
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "id_ed25519")
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
