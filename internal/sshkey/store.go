package sshkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

type Store struct {
	dir string
}

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

func (s *Store) PublicKey() (string, error) {
	fileMu.Lock()
	defer fileMu.Unlock()

	if err := ensurePermissions(s.dir); err != nil {
		return "", err
	}
	signer, err := s.signer()
	if errors.Is(err, os.ErrNotExist) {
		return s.rotate()
	}
	if err != nil {
		return "", err
	}
	return s.writePublicKey(signer.PublicKey())
}

func (s *Store) Rotate() (string, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	return s.rotate()
}

func (s *Store) Signer() (ssh.Signer, error) {
	fileMu.Lock()
	defer fileMu.Unlock()

	if err := ensurePermissions(s.dir); err != nil {
		return nil, err
	}
	signer, err := s.signer()
	if errors.Is(err, os.ErrNotExist) {
		if _, err := s.rotate(); err != nil {
			return nil, err
		}
		return s.signer()
	}
	return signer, err
}

func (s *Store) signer() (ssh.Signer, error) {
	privateKey, err := os.ReadFile(filepath.Join(s.dir, "id_ed25519"))
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(privateKey)
}

func (s *Store) rotate() (string, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		return "", err
	}
	if err := writeFileAtomic(filepath.Join(s.dir, "id_ed25519"), pem.EncodeToMemory(block), 0o600); err != nil {
		return "", err
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return "", err
	}
	return s.writePublicKey(signer.PublicKey())
}

func (s *Store) writePublicKey(key ssh.PublicKey) (string, error) {
	authorizedKey := ssh.MarshalAuthorizedKey(key)
	if err := writeFileAtomic(filepath.Join(s.dir, "id_ed25519.pub"), authorizedKey, 0o644); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(authorizedKey)), nil
}

func ensurePermissions(dir string) error {
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	for path, mode := range map[string]os.FileMode{
		filepath.Join(dir, "id_ed25519"):     0o600,
		filepath.Join(dir, "id_ed25519.pub"): 0o644,
	} {
		if err := os.Chmod(path, mode); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
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
