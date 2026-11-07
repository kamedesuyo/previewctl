//go:build linux || darwin

package main

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

func detach(cmd *exec.Cmd)                        { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func hideWindow(cmd *exec.Cmd)                    {}
func samePath(a, b string) bool                   { return a == b }
func openStateFile(path string) (*os.File, error) { return os.Open(path) }
func replaceStateFile(from, to string) error      { return os.Rename(from, to) }
func shellName() string                           { return "/bin/sh" }
func shellCommand(s *store, id, command string) (*exec.Cmd, error) {
	return exec.Command("/bin/sh", "-c", command), nil
}
func shutdownSignals() []os.Signal                { return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP} }
func shutdownGrace(d time.Duration) time.Duration { return d }
func beginShutdown()                              { _ = syscall.Kill(-os.Getpid(), syscall.SIGTERM) }
func createCapsule() (func(), error) {
	group, err := syscall.Getpgid(0)
	if err != nil {
		return nil, err
	}
	if group != os.Getpid() {
		return nil, errors.New("supervisor does not own its process group")
	}
	// 自分がグループの先頭に残ることで、停止直前のPID再利用を防ぐ。
	return func() { _ = syscall.Kill(-os.Getpid(), syscall.SIGKILL) }, nil
}
func tryLock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }
func unlockFile(f *os.File)    { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
func openBrowser(url string) error {
	if runtime.GOOS == "darwin" {
		return runBrowser("open", url)
	}
	return runBrowser("xdg-open", url)
}
