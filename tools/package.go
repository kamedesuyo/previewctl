//go:build ignore

package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func main() {
	if err := build(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func build() error {
	version := flag.String("version", "", "release version")
	target := flag.String("target", "", "one OS/architecture, or all when omitted")
	flag.Parse()
	source, err := os.ReadFile("main.go")
	if err != nil {
		return err
	}
	match := regexp.MustCompile(`var version = "([^"]+)"`).FindSubmatch(source)
	if len(match) != 2 {
		return fmt.Errorf("cannot read version")
	}
	if *version == "" {
		*version = string(match[1])
	}
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$`).MatchString(*version) {
		return fmt.Errorf("invalid version")
	}
	if *version != string(match[1]) {
		return fmt.Errorf("release version %s differs from main.go %s", *version, match[1])
	}
	targets := []string{"windows/amd64", "windows/arm64", "darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"}
	if *target != "" {
		found := false
		for _, candidate := range targets {
			if candidate == *target {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("unsupported target: %s", *target)
		}
		targets = []string{*target}
	}
	if err := os.MkdirAll("dist", 0755); err != nil {
		return err
	}
	var checksums []string
	for _, target := range targets {
		parts := strings.Split(target, "/")
		goos, arch := parts[0], parts[1]
		stage, err := os.MkdirTemp("", "previewctl-package-")
		if err != nil {
			return err
		}
		binary := "previewctl"
		installer := "install.sh"
		extension := ".tar.gz"
		if goos == "windows" {
			binary += ".exe"
			installer = "install.ps1"
			extension = ".zip"
		}
		cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -X main.version="+*version, "-o", filepath.Join(stage, binary), ".")
		for _, v := range os.Environ() {
			key := strings.ToUpper(strings.SplitN(v, "=", 2)[0])
			if key != "GOOS" && key != "GOARCH" && key != "CGO_ENABLED" {
				cmd.Env = append(cmd.Env, v)
			}
		}
		cmd.Env = append(cmd.Env, "GOOS="+goos, "GOARCH="+arch, "CGO_ENABLED=0")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			os.RemoveAll(stage)
			return err
		}
		for _, file := range []string{"README.md", "LICENSE", installer} {
			b, err := os.ReadFile(file)
			if err != nil {
				os.RemoveAll(stage)
				return err
			}
			if err := os.WriteFile(filepath.Join(stage, file), b, 0644); err != nil {
				os.RemoveAll(stage)
				return err
			}
		}
		name := fmt.Sprintf("previewctl_%s_%s_%s%s", *version, goos, arch, extension)
		path := filepath.Join("dist", name)
		err = archive(path, stage, []string{binary, "README.md", "LICENSE", installer}, goos == "windows")
		os.RemoveAll(stage)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		sum := sha256.New()
		_, err = io.Copy(sum, f)
		f.Close()
		if err != nil {
			return err
		}
		checksums = append(checksums, fmt.Sprintf("%x  %s", sum.Sum(nil), name))
		fmt.Println(name)
	}
	sort.Strings(checksums)
	return os.WriteFile(filepath.Join("dist", "checksums.txt"), []byte(strings.Join(checksums, "\n")+"\n"), 0644)
}

func archive(path, stage string, files []string, windows bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if windows {
		writer := zip.NewWriter(f)
		for _, name := range files {
			b, err := os.ReadFile(filepath.Join(stage, name))
			if err != nil {
				return err
			}
			header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: stamp}
			header.SetMode(0644)
			out, err := writer.CreateHeader(header)
			if err != nil {
				return err
			}
			if _, err := out.Write(b); err != nil {
				return err
			}
		}
		return writer.Close()
	}
	gz := gzip.NewWriter(f)
	writer := tar.NewWriter(gz)
	for _, name := range files {
		b, err := os.ReadFile(filepath.Join(stage, name))
		if err != nil {
			return err
		}
		mode := int64(0644)
		if name == "previewctl" || name == "install.sh" {
			mode = 0755
		}
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(b)), ModTime: stamp}); err != nil {
			return err
		}
		if _, err := writer.Write(b); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return gz.Close()
}
