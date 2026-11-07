package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type entry struct {
	Schema        int           `json:"schemaVersion"`
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Status        string        `json:"status"`
	Port          int           `json:"port"`
	URL           string        `json:"url"`
	Cwd           string        `json:"cwd"`
	Command       string        `json:"command"`
	StartedAt     time.Time     `json:"startedAt"`
	ExpiresAt     *time.Time    `json:"expiresAt"`
	StoppedAt     *time.Time    `json:"stoppedAt,omitempty"`
	Pinned        bool          `json:"pinned"`
	StdoutLog     string        `json:"stdoutLog"`
	StderrLog     string        `json:"stderrLog"`
	SupervisorPID int           `json:"supervisorPid"`
	ExitCode      *int          `json:"exitCode,omitempty"`
	Reason        string        `json:"reason,omitempty"`
	Control       string        `json:"control,omitempty"`
	Token         string        `json:"token,omitempty"`
	Grace         time.Duration `json:"grace"`
}

type store struct{ root string }

func openStore(root string) (*store, error) {
	if root == "" {
		root = os.Getenv("PREVIEWCTL_STATE_DIR")
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		switch runtime.GOOS {
		case "windows":
			base := os.Getenv("LOCALAPPDATA")
			if base == "" {
				base = filepath.Join(home, "AppData", "Local")
			}
			root = filepath.Join(base, "PreviewCtl")
		case "darwin":
			root = filepath.Join(home, "Library", "Application Support", "previewctl")
		default:
			base := os.Getenv("XDG_STATE_HOME")
			if !filepath.IsAbs(base) {
				base = filepath.Join(home, ".local", "state")
			}
			root = filepath.Join(base, "previewctl")
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for _, p := range []string{root, filepath.Join(root, "previews"), filepath.Join(root, "logs"), filepath.Join(root, "runs")} {
		if err := os.MkdirAll(p, 0700); err != nil {
			return nil, err
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(p, 0700); err != nil {
				return nil, err
			}
		}
	}
	return &store{root}, nil
}

func validID(id string) bool {
	if len(id) != 24 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func (s *store) entryPath(id string) string { return filepath.Join(s.root, "previews", id+".json") }
func (s *store) read(id string) (*entry, error) {
	if !validID(id) {
		return nil, errors.New("invalid preview ID")
	}
	f, err := openStateFile(s.entryPath(id))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var e entry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, err
	}
	if e.Schema != 1 || e.ID != id {
		return nil, errors.New("invalid preview record")
	}
	return &e, nil
}
func (s *store) write(e *entry) error {
	if !validID(e.ID) {
		return errors.New("invalid preview ID")
	}
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Join(s.root, "previews"), ".entry-*")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
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
	return replaceStateFile(path, s.entryPath(e.ID))
}
func (s *store) readAll() ([]*entry, error) {
	files, err := os.ReadDir(filepath.Join(s.root, "previews"))
	if err != nil {
		return nil, err
	}
	entries := make([]*entry, 0, len(files))
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		e, err := s.read(strings.TrimSuffix(file.Name(), ".json"))
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", file.Name(), err)
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].StartedAt.Before(entries[j].StartedAt) })
	return entries, nil
}
func (s *store) lock() (func(), error) {
	f, err := os.OpenFile(filepath.Join(s.root, "state.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		err = tryLock(f)
		if err == nil {
			return func() { unlockFile(f); f.Close() }, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("state is busy: %w", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
