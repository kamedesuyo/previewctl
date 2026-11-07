package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var createJobObject = kernel32.NewProc("CreateJobObjectW")
var setInformationJobObject = kernel32.NewProc("SetInformationJobObject")
var assignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
var lockFileEx = kernel32.NewProc("LockFileEx")
var unlockFileEx = kernel32.NewProc("UnlockFileEx")
var replaceFile = kernel32.NewProc("ReplaceFileW")

type jobLimits struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}
type ioCounters struct{ ReadOperationCount, WriteOperationCount, OtherOperationCount, ReadTransferCount, WriteTransferCount, OtherTransferCount uint64 }
type extendedJobLimits struct {
	BasicLimitInformation                                                        jobLimits
	IoInfo                                                                       ioCounters
	ProcessMemoryLimit, JobMemoryLimit, PeakProcessMemoryUsed, PeakJobMemoryUsed uintptr
}

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008 | 0x00000200}
}
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func samePath(a, b string) bool { return strings.EqualFold(a, b) }
func openStateFile(path string) (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	// os.Openの共有モードでは、一覧読み取り中の原子的な置換がWindowsで失敗する。
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}
func replaceStateFile(from, to string) error {
	fromPtr, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	toPtr, err := syscall.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		ok, _, err := replaceFile.Call(uintptr(unsafe.Pointer(toPtr)), uintptr(unsafe.Pointer(fromPtr)), 0, 0, 0, 0)
		if ok != 0 {
			return nil
		}
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return os.Rename(from, to)
		}
		if time.Now().After(deadline) || (!errors.Is(err, syscall.Errno(32)) && !errors.Is(err, syscall.ERROR_ACCESS_DENIED)) {
			return &os.PathError{Op: "replace", Path: to, Err: err}
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func shellName() string {
	if shell := os.Getenv("ComSpec"); shell != "" {
		return shell
	}
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
}
func shellCommand(s *store, id, command string) (*exec.Cmd, error) {
	path := filepath.Join(s.root, "runs", id+".cmd")
	if err := os.WriteFile(path, []byte("@echo off\r\nchcp 65001 >nul\r\nsetlocal DisableDelayedExpansion\r\n"+command+"\r\n"), 0600); err != nil {
		return nil, err
	}
	cmd := exec.Command(shellName())
	hideWindow(cmd)
	// cmd.exe独自の引用規則により、空白や日本語を含むスクリプトパスを二重に囲む。
	cmd.SysProcAttr.CmdLine = syscall.EscapeArg(shellName()) + ` /d /s /c ""` + path + `""`
	return cmd, nil
}
func shutdownSignals() []os.Signal                { return []os.Signal{os.Interrupt} }
func shutdownGrace(d time.Duration) time.Duration { return 0 }
func beginShutdown()                              {}
func createCapsule() (func(), error) {
	h, _, err := createJobObject.Call(0, 0)
	if h == 0 {
		return nil, fmt.Errorf("CreateJobObject: %w", err)
	}
	job := syscall.Handle(h)
	limits := extendedJobLimits{}
	limits.BasicLimitInformation.LimitFlags = 0x2000
	ok, _, err := setInformationJobObject.Call(h, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		syscall.CloseHandle(job)
		return nil, fmt.Errorf("SetInformationJobObject: %w", err)
	}
	current, err := syscall.GetCurrentProcess()
	if err != nil {
		syscall.CloseHandle(job)
		return nil, err
	}
	// 起動前に自分をJobへ入れ、子プロセスが管理対象外で走る隙間を作らない。
	ok, _, err = assignProcessToJobObject.Call(h, uintptr(current))
	if ok == 0 {
		syscall.CloseHandle(job)
		return nil, fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	return func() { _ = syscall.CloseHandle(job) }, nil
}
func tryLock(f *os.File) error {
	var overlapped syscall.Overlapped
	ok, _, err := lockFileEx.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ok == 0 {
		return err
	}
	return nil
}
func unlockFile(f *os.File) {
	var overlapped syscall.Overlapped
	_, _, _ = unlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
}
func openBrowser(url string) error {
	return runBrowser("rundll32.exe", "url.dll,FileProtocolHandler", url)
}
