//go:build ignore

package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	if err := verify(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func verify() error {
	platform := runtime.GOOS + "/" + runtime.GOARCH
	if expected := os.Getenv("PREVIEWCTL_EXPECT_PLATFORM"); expected != "" && expected != platform {
		return fmt.Errorf("runner is %s, expected %s", platform, expected)
	}
	fmt.Println("Runtime platform:", platform)
	checksums, err := os.ReadFile("dist/checksums.txt")
	if err != nil {
		return err
	}
	suffix := "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	if runtime.GOOS == "windows" {
		suffix = "_windows_" + runtime.GOARCH + ".zip"
	}
	archive, expected := "", ""
	for _, line := range strings.Split(string(checksums), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && strings.HasSuffix(parts[1], suffix) {
			if archive != "" {
				return fmt.Errorf("multiple archives for %s", platform)
			}
			archive, expected = parts[1], parts[0]
		}
	}
	if archive == "" || filepath.Base(archive) != archive {
		return fmt.Errorf("missing native archive")
	}
	path := filepath.Join("dist", archive)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, f)
	f.Close()
	if copyErr != nil {
		return copyErr
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != expected {
		return fmt.Errorf("checksum mismatch: %s", archive)
	}
	root, err := os.MkdirTemp("", "previewctl-release-日本語 ")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	stage := filepath.Join(root, "extracted")
	installed := filepath.Join(root, "installed 日本語")
	if err := os.MkdirAll(stage, 0700); err != nil {
		return err
	}
	binary, installer := "previewctl", "install.sh"
	if runtime.GOOS == "windows" {
		binary, installer = "previewctl.exe", "install.ps1"
	}
	wanted := map[string]bool{binary: false, installer: false, "README.md": false, "LICENSE": false}
	write := func(name string, mode os.FileMode, r io.Reader) error {
		seen, ok := wanted[name]
		if !ok || seen {
			return fmt.Errorf("unexpected archive entry: %s", name)
		}
		out, err := os.OpenFile(filepath.Join(stage, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
		if err != nil {
			return err
		}
		_, err = io.Copy(out, r)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		wanted[name] = true
		return nil
	}
	if runtime.GOOS == "windows" {
		z, err := zip.OpenReader(path)
		if err != nil {
			return err
		}
		for _, item := range z.File {
			r, err := item.Open()
			if err != nil {
				z.Close()
				return err
			}
			err = write(item.Name, item.Mode(), r)
			r.Close()
			if err != nil {
				z.Close()
				return err
			}
		}
		z.Close()
	} else {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		t := tar.NewReader(gz)
		for {
			header, err := t.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if header.Typeflag != tar.TypeReg {
				return fmt.Errorf("non-file archive entry: %s", header.Name)
			}
			if err := write(header.Name, os.FileMode(header.Mode), t); err != nil {
				return err
			}
		}
	}
	for name, found := range wanted {
		if !found {
			return fmt.Errorf("archive is missing %s", name)
		}
	}
	var install *exec.Cmd
	if runtime.GOOS == "windows" {
		install = exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(stage, installer), "-InstallDir", installed, "-NoPathUpdate")
	} else {
		install = exec.Command("sh", filepath.Join(stage, installer))
		install.Env = environment("PREVIEWCTL_INSTALL_DIR", installed)
	}
	install.Stdout, install.Stderr = os.Stdout, os.Stderr
	if err := install.Run(); err != nil {
		return fmt.Errorf("install: %w", err)
	}
	version := exec.Command(filepath.Join(installed, binary), "version")
	version.Stdout, version.Stderr = os.Stdout, os.Stderr
	if err := version.Run(); err != nil {
		return fmt.Errorf("installed binary: %w", err)
	}
	tests := exec.Command("go", "test", "-count=1", "-v", "./...")
	tests.Env = environment("PREVIEWCTL_TEST_BINARY", filepath.Join(installed, binary))
	tests.Stdout, tests.Stderr = os.Stdout, os.Stderr
	return tests.Run()
}

func environment(key, value string) []string {
	var result []string
	for _, item := range os.Environ() {
		if !strings.EqualFold(strings.SplitN(item, "=", 2)[0], key) {
			result = append(result, item)
		}
	}
	return append(result, key+"="+value)
}
