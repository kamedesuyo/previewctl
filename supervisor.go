package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"
)

func supervise(s *store, id string) (resultErr error) {
	e, err := s.read(id)
	if err != nil {
		return err
	}
	secret := os.Getenv("PREVIEWCTL_INTERNAL_TOKEN")
	_ = os.Unsetenv("PREVIEWCTL_INTERNAL_TOKEN")
	if len(secret) != 64 || subtle.ConstantTimeCompare([]byte(secret), []byte(e.Token)) != 1 || e.SupervisorPID != 0 {
		return errors.New("unauthorized supervisor invocation")
	}
	e.SupervisorPID = os.Getpid()
	endCapsule, err := createCapsule()
	if err != nil {
		e.Status = "failed"
		e.Reason = err.Error()
		_ = s.write(e)
		return err
	}
	defer endCapsule()
	var mu sync.Mutex
	defer func() {
		if resultErr != nil {
			mu.Lock()
			e.Status = "failed"
			e.Reason = resultErr.Error()
			_ = s.write(e)
			mu.Unlock()
		}
	}()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	e.Control = listener.Addr().String()
	stop := make(chan string, 1)
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 3 * time.Second, MaxHeaderBytes: 4096}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+secret)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/stop" {
			select {
			case stop <- "manual-stop":
			default:
			}
		} else if r.Method != "GET" || r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		copy := *e
		mu.Unlock()
		copy.Token = ""
		copy.Control = ""
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(copy)
	})
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	stdout, err := os.OpenFile(e.StdoutLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer stdout.Close()
	stderr, err := os.OpenFile(e.StderrLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer stderr.Close()
	command := strings.NewReplacer("{port}", fmt.Sprint(e.Port), "{host}", "127.0.0.1", "{url}", e.URL).Replace(e.Command)
	cmd, err := shellCommand(s, e.ID, command)
	if err != nil {
		return err
	}
	cmd.Dir = e.Cwd
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append(filteredEnvironment(os.Environ(), "PORT", "HOST", "PREVIEW_URL", "PREVIEWCTL_INTERNAL_TOKEN"), fmt.Sprintf("PORT=%d", e.Port), "HOST=127.0.0.1", "PREVIEW_URL="+e.URL)
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, shutdownSignals()...)
	defer signal.Stop(signals)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	mu.Lock()
	err = s.write(e)
	mu.Unlock()
	if err != nil {
		return err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var expiry <-chan time.Time
	if e.ExpiresAt != nil {
		timer := time.NewTimer(time.Until(*e.ExpiresAt))
		defer timer.Stop()
		expiry = timer.C
	}
	status, reason := "stopped", ""
	var exitCode *int
loop:
	for {
		select {
		case reason = <-stop:
			break loop
		case <-signals:
			reason = "signal"
			break loop
		case <-expiry:
			reason = "ttl-expired"
			break loop
		case err := <-done:
			code := cmd.ProcessState.ExitCode()
			exitCode = &code
			status = "exited"
			reason = "command-exited"
			if err != nil {
				status = "failed"
				reason = err.Error()
			}
			break loop
		case <-ticker.C:
			mu.Lock()
			running := e.Status == "running"
			mu.Unlock()
			if !running && isListening(e.Port) {
				mu.Lock()
				e.Status = "running"
				err = s.write(e)
				mu.Unlock()
				if err != nil {
					return err
				}
			}
		}
	}
	mu.Lock()
	e.Status = "stopping"
	e.Reason = reason
	err = s.write(e)
	mu.Unlock()
	if err != nil {
		return err
	}
	beginShutdown()
	if grace := shutdownGrace(e.Grace); grace > 0 {
		time.Sleep(grace)
	}
	mu.Lock()
	now := time.Now().UTC()
	e.Status = status
	e.Reason = reason
	e.StoppedAt = &now
	e.ExitCode = exitCode
	err = s.write(e)
	mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
	return err
}
