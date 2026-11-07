package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var controlClient = &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}

func active(status string) bool {
	return status == "starting" || status == "running" || status == "stopping"
}

func control(e *entry, method, path string) (*entry, error) {
	host, _, err := net.SplitHostPort(e.Control)
	if err != nil || host != "127.0.0.1" || len(e.Token) != 64 {
		return nil, errors.New("invalid control endpoint")
	}
	req, err := http.NewRequest(method, "http://"+e.Control+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.Token)
	res, err := controlClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("supervisor returned %s", res.Status)
	}
	var fresh entry
	if err := json.NewDecoder(io.LimitReader(res.Body, 128*1024)).Decode(&fresh); err != nil {
		return nil, err
	}
	if fresh.ID != e.ID {
		return nil, errors.New("supervisor identity mismatch")
	}
	fresh.Token = e.Token
	fresh.Control = e.Control
	return &fresh, nil
}

func refresh(e *entry) *entry {
	if !active(e.Status) {
		return e
	}
	if fresh, err := control(e, "GET", "/status"); err == nil {
		return fresh
	}
	copy := *e
	copy.Status = "unreachable"
	return &copy
}

func allocatePort(start, end int, reserved map[int]bool) (int, error) {
	for p := start; p <= end; p++ {
		if reserved[p] {
			continue
		}
		l, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			l.Close()
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free port in %d–%d", start, end)
}

func isListening(port int) bool {
	conn, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func startPreview(s *store, o options) (*entry, error) {
	if strings.TrimSpace(o.run) == "" {
		return nil, errors.New("start requires --run")
	}
	if strings.ContainsAny(o.run, "\x00\r\n") {
		return nil, errors.New("--run must be one command line without NUL or newlines")
	}
	if o.cwd == "" {
		var err error
		o.cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	cwd, err := filepath.Abs(o.cwd)
	if err != nil {
		return nil, err
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("--cwd is not a directory")
	}
	if o.name == "" {
		o.name = filepath.Base(cwd)
	}
	if strings.TrimSpace(o.name) == "" || strings.ContainsAny(o.name, "\r\n\t\x00") {
		return nil, errors.New("invalid preview name")
	}
	unlock, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	entries, err := s.readAll()
	if err != nil {
		return nil, err
	}
	reserved := map[int]bool{}
	for _, old := range entries {
		e := refresh(old)
		if e.Name == o.name && samePath(e.Cwd, cwd) && (active(e.Status) || e.Status == "unreachable") {
			if e.Status == "unreachable" {
				return nil, fmt.Errorf("%s has an unreachable supervisor; inspect the existing state before restarting", e.ID)
			}
			if o.force {
				if err := stopPreview(s, e); err != nil {
					return nil, err
				}
			} else {
				if e.Command != o.run {
					return nil, errors.New("same name and directory already use another command; use --force to restart")
				}
				return e, nil
			}
		} else if active(e.Status) || e.Status == "unreachable" {
			reserved[e.Port] = true
		}
	}
	start, end := o.portStart, o.portEnd
	if o.port != 0 {
		start, end = o.port, o.port
	}
	port, err := allocatePort(start, end, reserved)
	if err != nil {
		return nil, err
	}
	id, err := randomHex(12)
	if err != nil {
		return nil, err
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var expires *time.Time
	if !o.pinned {
		v := now.Add(o.ttl)
		expires = &v
	}
	e := &entry{Schema: 1, ID: id, Name: o.name, Status: "starting", Port: port, URL: fmt.Sprintf("http://127.0.0.1:%d/", port), Cwd: cwd, Command: o.run, StartedAt: now, ExpiresAt: expires, Pinned: o.pinned, Token: token, Grace: o.grace, StdoutLog: filepath.Join(s.root, "logs", id+".out.log"), StderrLog: filepath.Join(s.root, "logs", id+".err.log")}
	if err := s.write(e); err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	log, err := os.OpenFile(filepath.Join(s.root, "logs", id+".supervisor.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	defer log.Close()
	cmd := exec.Command(exe, "__supervise", "--state-dir", s.root, "--id", id)
	cmd.Env = append(filteredEnvironment(os.Environ(), "PREVIEWCTL_INTERNAL_TOKEN"), "PREVIEWCTL_INTERNAL_TOKEN="+token)
	cmd.Stdin = nil
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.Dir = cwd
	detach(cmd)
	if err := cmd.Start(); err != nil {
		e.Status = "failed"
		e.Reason = err.Error()
		_ = s.write(e)
		return e, err
	}
	_ = cmd.Process.Release()
	deadline := time.Now().Add(o.wait)
	for time.Now().Before(deadline) {
		fresh, err := s.read(id)
		if err != nil {
			return nil, err
		}
		e = fresh
		if e.Status == "running" {
			return e, nil
		}
		if !active(e.Status) {
			return e, fmt.Errorf("preview %s: %s; use previewctl logs --id %s", e.Status, e.Reason, id)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if e.Control == "" {
		return e, fmt.Errorf("supervisor did not initialize; inspect %s", log.Name())
	}
	return e, nil
}

func stopPreview(s *store, e *entry) error {
	if _, err := control(e, "POST", "/stop"); err != nil {
		return fmt.Errorf("%s: refusing to stop without its supervisor: %w", e.ID, err)
	}
	deadline := time.Now().Add(e.Grace + 5*time.Second)
	for time.Now().Before(deadline) {
		fresh, err := s.read(e.ID)
		if err != nil {
			return err
		}
		if !active(fresh.Status) {
			if _, err := control(e, "GET", "/status"); err != nil {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("%s: supervisor has not finished stopping", e.ID)
}

func filteredEnvironment(env []string, keys ...string) []string {
	result := make([]string, 0, len(env))
	for _, value := range env {
		key := strings.SplitN(value, "=", 2)[0]
		keep := true
		for _, exclude := range keys {
			if strings.EqualFold(key, exclude) {
				keep = false
				break
			}
		}
		if keep {
			result = append(result, value)
		}
	}
	return result
}

func runBrowser(command string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	hideWindow(cmd)
	return cmd.Run()
}
