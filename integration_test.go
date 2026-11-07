package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var testBinary string

func TestMain(m *testing.M) {
	if mode := os.Getenv("PREVIEWCTL_TEST_FIXTURE"); mode != "" {
		runFixture(mode)
		return
	}
	if binary := os.Getenv("PREVIEWCTL_TEST_BINARY"); binary != "" {
		var err error
		testBinary, err = filepath.Abs(binary)
		if err != nil {
			panic(err)
		}
		fmt.Printf("Testing release binary on %s/%s: %s\n", runtime.GOOS, runtime.GOARCH, testBinary)
		os.Exit(m.Run())
	}
	temp, err := os.MkdirTemp("", "previewctl-test-build-")
	if err != nil {
		panic(err)
	}
	testBinary = filepath.Join(temp, "previewctl")
	if runtime.GOOS == "windows" {
		testBinary += ".exe"
	}
	build := exec.Command("go", "build", "-o", testBinary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v", out, err)
		os.RemoveAll(temp)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(temp)
	os.Exit(code)
}

func runFixture(mode string) {
	if mode == "fail" {
		fmt.Fprintln(os.Stderr, "fixture failure 日本語")
		os.Exit(7)
	}
	if mode == "leaf" {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(os.Getenv("PREVIEWCTL_TEST_CHILD_FILE"), []byte(listener.Addr().String()), 0600); err != nil {
			panic(err)
		}
		_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "leaf") }))
		return
	}
	if mode == "tree" {
		exe, _ := os.Executable()
		child := exec.Command(exe)
		child.Env = append(filteredEnvironment(os.Environ(), "PREVIEWCTL_TEST_FIXTURE"), "PREVIEWCTL_TEST_FIXTURE=leaf")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		hideWindow(child)
		if err := child.Start(); err != nil {
			panic(err)
		}
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", os.Getenv("PORT")))
	if err != nil {
		panic(err)
	}
	fmt.Fprintln(os.Stdout, "fixture ready 日本語")
	fmt.Fprintln(os.Stderr, "fixture stderr")
	_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cwd, _ := os.Getwd()
		_ = json.NewEncoder(w).Encode(map[string]string{"cwd": cwd, "host": os.Getenv("HOST"), "url": os.Getenv("PREVIEW_URL"), "token": os.Getenv("PREVIEWCTL_INTERNAL_TOKEN")})
	}))
}

func testSetup(t *testing.T, mode string) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "日本語 state space")
	cwd := filepath.Join(t.TempDir(), "日本語 project space")
	if err := os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	cwd = resolved
	t.Setenv("PREVIEWCTL_TEST_FIXTURE", mode)
	t.Cleanup(func() { _, _ = cli(root, "stop", "--all"); _, _ = cli(root, "cleanup") })
	return root, cwd
}

func cli(state string, args ...string) ([]byte, error) {
	args = append(args, "--state-dir", state)
	cmd := exec.Command(testBinary, args...)
	cmd.Env = filteredEnvironment(os.Environ(), "PREVIEWCTL_TEST_FIXTURE")
	cmd.Env = append(cmd.Env, "PREVIEWCTL_TEST_FIXTURE="+os.Getenv("PREVIEWCTL_TEST_FIXTURE"))
	return cmd.CombinedOutput()
}

func fixtureCommand(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return `"` + exe + `"`
}

func startTest(t *testing.T, root, cwd, name string, extra ...string) entry {
	t.Helper()
	args := []string{"start", "--name", name, "--cwd", cwd, "--run", fixtureCommand(t), "--json", "--wait", "8s", "--grace", "100ms"}
	args = append(args, extra...)
	out, err := cli(root, args...)
	if err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	var rows []entry
	if err := json.Unmarshal(out, &rows); err != nil || len(rows) != 1 {
		t.Fatalf("start output: %s (%v)", out, err)
	}
	if rows[0].Status != "running" {
		t.Fatalf("not running: %s", out)
	}
	if rows[0].Token != "" || rows[0].Control != "" {
		t.Fatal("JSON exposed control credentials")
	}
	return rows[0]
}

func waitUntil(t *testing.T, description string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("timed out: " + description)
}

func TestLifecycleUnicodeAndProcessTree(t *testing.T) {
	root, cwd := testSetup(t, "tree")
	childFile := filepath.Join(cwd, "child.txt")
	t.Setenv("PREVIEWCTL_TEST_CHILD_FILE", childFile)
	if out, err := cli(root, "list", "--json"); err != nil || strings.TrimSpace(string(out)) != "[]" {
		t.Fatalf("empty list: %s %v", out, err)
	}
	e := startTest(t, root, cwd, "日本語 preview", "--pinned")
	response, err := controlClient.Get(e.URL)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]string
	err = json.NewDecoder(response.Body).Decode(&body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(body["cwd"], cwd) || body["host"] != "127.0.0.1" || body["url"] != e.URL || body["token"] != "" {
		t.Fatalf("wrong environment: %#v", body)
	}
	second := startTest(t, root, cwd, "日本語 preview", "--pinned")
	if second.ID != e.ID {
		t.Fatal("duplicate preview was started")
	}
	logs, err := cli(root, "logs", "--id", e.ID, "--json")
	if err != nil || !strings.Contains(string(logs), "fixture ready 日本語") || !strings.Contains(string(logs), "fixture stderr") {
		t.Fatalf("logs: %s %v", logs, err)
	}
	s, _ := openStore(root)
	private, err := s.read(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	unauthorized, err := controlClient.Post("http://"+private.Control+"/stop", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != 401 {
		t.Fatalf("unauthorized stop returned %d", unauthorized.StatusCode)
	}
	waitUntil(t, "child starts", func() bool { _, err := os.Stat(childFile); return err == nil })
	childAddr, _ := os.ReadFile(childFile)
	if out, err := cli(root, "stop", "--id", e.ID, "--json"); err != nil {
		t.Fatalf("stop: %s %v", out, err)
	}
	waitUntil(t, "root and child stop", func() bool {
		conn, err := net.DialTimeout("tcp", string(childAddr), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return false
		}
		return !isListening(e.Port)
	})
	if out, err := cli(root, "cleanup", "--json"); err != nil || !strings.Contains(string(out), "removed") {
		t.Fatalf("cleanup: %s %v", out, err)
	}
	if _, err := os.Stat(e.StdoutLog); err != nil {
		t.Fatal("cleanup removed logs")
	}
	if out, err := cli(root, "list", "--all", "--json"); err != nil || strings.TrimSpace(string(out)) != "[]" {
		t.Fatalf("clean list: %s %v", out, err)
	}
}

func TestExpiryAndPinned(t *testing.T) {
	root, cwd := testSetup(t, "serve")
	expires := startTest(t, root, cwd, "expires", "--ttl", "1200ms")
	pinned := startTest(t, root, cwd, "pinned", "--ttl", "1200ms", "--pinned")
	s, _ := openStore(root)
	waitUntil(t, "TTL finishes stopping preview", func() bool {
		e, err := s.read(expires.ID)
		return err == nil && e.Status == "stopped" && !isListening(expires.Port)
	})
	if !isListening(pinned.Port) {
		t.Fatal("pinned preview expired")
	}
	e, err := s.read(expires.ID)
	if err != nil || e.Reason != "ttl-expired" || e.Status != "stopped" {
		t.Fatalf("expiry state does not record completion: %v", err)
	}
}

func TestConcurrentStartAndOccupiedPort(t *testing.T) {
	root, cwd := testSetup(t, "serve")
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port
	out, err := cli(root, "start", "--name", "occupied", "--cwd", cwd, "--run", fixtureCommand(t), "--port", fmt.Sprint(port))
	if err == nil {
		t.Fatalf("occupied port accepted: %s", out)
	}
	var wg sync.WaitGroup
	results := make(chan []byte, 4)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := cli(root, "start", "--name", "concurrent", "--cwd", cwd, "--run", fixtureCommand(t), "--json", "--grace", "100ms")
			results <- out
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent start: %v", err)
		}
	}
	id := ""
	for out := range results {
		var entries []entry
		if err := json.Unmarshal(out, &entries); err != nil || len(entries) != 1 {
			t.Fatalf("concurrent output: %s", out)
		}
		if id != "" && id != entries[0].ID {
			t.Fatal("concurrent starts created duplicates")
		}
		id = entries[0].ID
	}
	second := startTest(t, root, cwd, "another")
	s, _ := openStore(root)
	first, _ := s.read(id)
	if second.Port == first.Port {
		t.Fatal("port allocated twice")
	}
	restarted := startTest(t, root, cwd, "another", "--force")
	if restarted.ID == second.ID {
		t.Fatal("force reused an old preview")
	}
}

func TestUnreachableDoesNotKillOrDelete(t *testing.T) {
	root, _ := testSetup(t, "serve")
	s, _ := openStore(root)
	id, _ := randomHex(12)
	token, _ := randomHex(32)
	e := &entry{Schema: 1, ID: id, Name: "unrelated", Status: "running", SupervisorPID: os.Getpid(), Token: token, Control: "127.0.0.1:0"}
	if err := s.write(e); err != nil {
		t.Fatal(err)
	}
	if out, err := cli(root, "stop", "--id", id); err == nil {
		t.Fatalf("unreachable supervisor accepted: %s", out)
	}
	if out, err := cli(root, "cleanup", "--json"); err != nil || !strings.Contains(string(out), "kept") {
		t.Fatalf("cleanup: %s %v", out, err)
	}
	if _, err := s.read(id); err != nil {
		t.Fatal("unreachable record removed")
	}
	if out, err := cli(root, "list", "--json"); err != nil || !strings.Contains(string(out), "unreachable") {
		t.Fatalf("unreachable list: %s %v", out, err)
	}
	_ = os.Remove(s.entryPath(id))
}

func TestStartupFailureAndLegacyOptions(t *testing.T) {
	root, cwd := testSetup(t, "fail")
	out, err := cli(root, "start", "-Name", "failure", "-Cwd", cwd, "-Run", fixtureCommand(t), "-Json", "-TtlMinutes", "2", "--grace", "0s")
	if err == nil || !strings.Contains(string(out), "failed") {
		t.Fatalf("startup failure: %s %v", out, err)
	}
	logs, err := cli(root, "logs", "--name", "failure", "--json")
	if err != nil || !strings.Contains(string(logs), "fixture failure 日本語") {
		t.Fatalf("failure logs: %s %v", logs, err)
	}
}

func TestArgumentValidation(t *testing.T) {
	for _, args := range [][]string{{"start", "--port", "65536"}, {"start", "--port-start", "42000", "--port-end", "41000"}, {"start", "--ttl", "0s"}, {"logs", "--tail", "-1"}, {"start", "--grace", "2m"}, {"start", "--unknown", "x"}, {"start", "--run"}} {
		if _, err := parseOptions(args); err == nil {
			t.Errorf("accepted invalid args: %v", args)
		}
	}
}

func TestStateReadersDoNotBlockReplacement(t *testing.T) {
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := randomHex(12)
	if err != nil {
		t.Fatal(err)
	}
	e := &entry{Schema: 1, ID: id, Status: "starting"}
	if err := s.write(e); err != nil {
		t.Fatal(err)
	}
	reader, err := openStateFile(s.entryPath(id))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	e.Status = "stopped"
	if err := s.write(e); err != nil {
		t.Fatalf("open reader prevented state replacement: %v", err)
	}
	previous, err := io.ReadAll(reader)
	if err != nil || !strings.Contains(string(previous), `"starting"`) {
		t.Fatalf("reader lost its original snapshot: %v", err)
	}
	current, err := s.read(id)
	if err != nil || current.Status != "stopped" {
		t.Fatalf("new reader did not get updated state: %v", err)
	}
}
