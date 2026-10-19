package baton

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

type Store struct {
	Root  string
	fault func(string) error // Isolated tests inject interruption at commit boundaries.
}

// StateDirEnv overrides the default state directory when --state-dir is omitted.
const StateDirEnv = "BATON_STATE_DIR"

func DefaultStateDir() (string, error) {
	if p := os.Getenv(StateDirEnv); p != "" {
		// A relative value would resolve differently in each workspace.
		if !filepath.IsAbs(p) {
			return "", fmt.Errorf("%s must be an absolute path", StateDirEnv)
		}
		return p, nil
	}
	p, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(p, "agent-handoff"), nil
}

func Open(path string) (*Store, error) {
	return openStore(path, true)
}

func OpenExisting(path string) (*Store, error) { return openStore(path, false) }

func openStore(path string, create bool) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultStateDir()
		if err != nil {
			return nil, err
		}
	}
	p, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if i, e := os.Lstat(p); e == nil && i.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("state root must not be a symlink")
	}
	if create {
		if err := os.MkdirAll(p, 0700); err != nil {
			return nil, err
		}
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return nil, err
	}
	s := &Store{Root: p}
	if err := s.guard(p); err != nil {
		return nil, err
	}
	return s, nil
}

// guard rejects state-internal links and permissive Unix modes. The private
// state root is a trust boundary, not protection against a malicious same user.
func (s *Store) guard(path string) error {
	if !within(s.Root, path) {
		return errors.New("path escapes state root")
	}
	for p := path; ; p = filepath.Dir(p) {
		i, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if i.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink in state path")
		}
		if !i.IsDir() && !i.Mode().IsRegular() {
			return errors.New("state path must be a regular file or directory")
		}
		if runtime.GOOS != "windows" && i.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("state path is not private: %s (requires 0700 directories/0600 files)", p)
		}
		if p == s.Root {
			break
		}
	}
	return nil
}

func (s *Store) mkdir(path string) error {
	if !within(s.Root, path) {
		return errors.New("path escapes state root")
	}
	if path != s.Root {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			if err := s.mkdir(filepath.Dir(path)); err != nil {
				return err
			}
			if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
		}
	}
	return s.guard(path)
}

func (s *Store) read(path string) ([]byte, error) {
	if err := s.guard(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("state data must be a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxJSON+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxJSON {
		return nil, errors.New("state file exceeds resource limit")
	}
	return b, nil
}

func (s *Store) readJSON(path string, v any) error {
	b, err := s.read(path)
	if err != nil {
		return err
	}
	if err := DecodeStrict(b, v); err != nil {
		return fmt.Errorf("invalid state %s: %w", path, err)
	}
	return nil
}

func (s *Store) writeJSON(path string, v any, replace bool) error {
	b, err := encode(v)
	if err != nil {
		return err
	}
	return s.atomicWrite(path, b, replace)
}

func (s *Store) atomicWrite(path string, b []byte, replace bool) error {
	dir := filepath.Dir(path)
	if err := s.guard(dir); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		if err := s.guard(path); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if filepath.Base(dir) == "revisions" {
		if err := s.boundary("revision_temp_synced"); err != nil {
			return err
		}
	}
	if replace {
		err = os.Rename(tmp, path)
	} else {
		err = os.Link(tmp, path)
	}
	if err != nil {
		return err
	}
	return syncDir(dir)
}

func (s *Store) lock() (func() error, error) {
	path := filepath.Join(s.Root, ".lock")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		b, e := s.read(path)
		if e != nil {
			return nil, e
		}
		return nil, fmt.Errorf("state is locked; do not steal an active lock; after all writers stop, unlock --lock-token %s", digest(b))
	}
	if err != nil {
		return nil, err
	}
	owner, err := newID()
	if err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	if _, err = f.WriteString(owner); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = syncDir(s.Root); err != nil {
		return nil, err
	}
	return func() error {
		b, err := s.read(path)
		if err != nil {
			return err
		}
		if string(b) != owner {
			return errors.New("lock changed; refusing to remove it")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		return syncDir(s.Root)
	}, nil
}

func (s *Store) mutate(fn func() error) error {
	release, err := s.lock()
	if err != nil {
		return err
	}
	err = fn()
	return errors.Join(err, release())
}

// Unlock is explicitly administrative. The caller must confirm all baton
// processes stopped. Digests prevent accidentally removing a different lock.
func (s *Store) Unlock(token string) error {
	path := filepath.Join(s.Root, ".lock")
	b, err := s.read(path)
	if err != nil {
		return err
	}
	if len(token) != 64 || digest(b) != token {
		return errors.New("lock token does not match; lock retained")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDir(s.Root)
}

func (s *Store) boundary(stage string) error {
	if s.fault != nil {
		return s.fault(stage)
	}
	return nil
}
