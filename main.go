package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

var version = "0.1.1"

type options struct {
	action, name, id, run, cwd, state string
	port, portStart, portEnd, tail    int
	ttl, wait, grace                  time.Duration
	pinned, json, all, force          bool
}

func parseOptions(args []string) (options, error) {
	o := options{action: "help"}
	if len(args) == 0 {
		return o, nil
	}
	o.action = strings.ToLower(args[0])
	if o.action == "--help" || o.action == "-h" {
		o.action = "help"
	}
	if o.action == "--version" {
		o.action = "version"
	}
	f := flag.NewFlagSet("previewctl "+o.action, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.StringVar(&o.name, "name", "", "")
	f.StringVar(&o.id, "id", "", "")
	f.StringVar(&o.run, "run", "", "")
	f.StringVar(&o.cwd, "cwd", "", "")
	f.StringVar(&o.state, "state-dir", "", "")
	f.IntVar(&o.port, "port", 0, "")
	f.IntVar(&o.portStart, "port-start", 41000, "")
	f.IntVar(&o.portEnd, "port-end", 41999, "")
	f.IntVar(&o.tail, "tail", 120, "")
	f.DurationVar(&o.ttl, "ttl", time.Hour, "")
	f.DurationVar(&o.wait, "wait", 10*time.Second, "")
	f.DurationVar(&o.grace, "grace", 2*time.Second, "")
	f.BoolVar(&o.pinned, "pinned", false, "")
	f.BoolVar(&o.json, "json", false, "")
	f.BoolVar(&o.all, "all", false, "")
	f.BoolVar(&o.force, "force", false, "")
	aliases := map[string]string{"portstart": "port-start", "portend": "port-end", "statedir": "state-dir"}
	bools := map[string]bool{"pinned": true, "json": true, "all": true, "force": true}
	var normalized []string
	for i := 1; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return o, fmt.Errorf("unexpected argument: %s", a)
		}
		parts := strings.SplitN(strings.TrimLeft(a, "-"), "=", 2)
		key := strings.ToLower(parts[0])
		if key == "h" || key == "help" {
			o.action = "help"
			return o, nil
		}
		if key == "ttlminutes" {
			value := ""
			if len(parts) == 2 {
				value = parts[1]
			} else if i+1 < len(args) {
				i++
				value = args[i]
			}
			minutes, err := strconv.Atoi(value)
			if err != nil || minutes <= 0 {
				return o, errors.New("TtlMinutes must be a positive integer")
			}
			normalized = append(normalized, "--ttl="+strconv.Itoa(minutes)+"m")
			continue
		}
		if alias, ok := aliases[key]; ok {
			key = alias
		}
		if len(parts) == 2 {
			normalized = append(normalized, "--"+key+"="+parts[1])
			continue
		}
		normalized = append(normalized, "--"+key)
		if !bools[key] {
			if i+1 >= len(args) {
				return o, fmt.Errorf("%s needs a value", a)
			}
			i++
			normalized = append(normalized, args[i])
		}
	}
	if err := f.Parse(normalized); err != nil {
		return o, err
	}
	if o.port < 0 || o.port > 65535 || o.portStart < 1 || o.portEnd > 65535 || o.portStart > o.portEnd {
		return o, errors.New("invalid port or port range")
	}
	if o.ttl <= 0 || o.wait <= 0 || o.grace < 0 || o.grace > time.Minute || o.tail < 0 {
		return o, errors.New("invalid duration or tail count")
	}
	return o, nil
}

func main() {
	if err := execute(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "previewctl:", err)
		os.Exit(1)
	}
}

func execute(args []string, out io.Writer) error {
	o, err := parseOptions(args)
	if err != nil {
		return err
	}
	if o.action == "help" {
		_, err = fmt.Fprint(out, helpText)
		return err
	}
	if o.action == "version" {
		_, err = fmt.Fprintln(out, "previewctl", version)
		return err
	}
	s, err := openStore(o.state)
	if err != nil {
		return err
	}
	if o.action == "__supervise" {
		return supervise(s, o.id)
	}
	switch o.action {
	case "start":
		e, err := startPreview(s, o)
		if e != nil {
			if outputErr := printEntries(out, []*entry{e}, o.json); outputErr != nil {
				return outputErr
			}
		}
		return err
	case "list":
		entries, err := s.readAll()
		if err != nil {
			return err
		}
		var selected []*entry
		for _, e := range entries {
			e = refresh(e)
			if o.all || active(e.Status) || e.Status == "unreachable" {
				selected = append(selected, e)
			}
		}
		return printEntries(out, selected, o.json)
	case "stop", "cleanup":
		unlock, err := s.lock()
		if err != nil {
			return err
		}
		defer unlock()
		entries, err := s.readAll()
		if err != nil {
			return err
		}
		if o.action == "stop" {
			entries, err = selectEntries(entries, o)
			if err != nil {
				return err
			}
		}
		results := make([]map[string]string, 0)
		var failures []error
		for _, original := range entries {
			e := refresh(original)
			if o.action == "cleanup" {
				if active(e.Status) && (e.ExpiresAt == nil || time.Now().Before(*e.ExpiresAt)) {
					continue
				}
				if e.Status == "unreachable" {
					results = append(results, map[string]string{"id": e.ID, "action": "kept", "reason": "supervisor unreachable; no process was terminated"})
					continue
				}
			}
			if active(e.Status) {
				if err := stopPreview(s, e); err != nil {
					failures = append(failures, err)
					continue
				}
			} else if e.Status == "unreachable" {
				failures = append(failures, fmt.Errorf("%s: supervisor unreachable; refusing PID-based termination", e.ID))
				continue
			}
			action := "stopped"
			if o.action == "cleanup" {
				if err := os.Remove(s.entryPath(e.ID)); err != nil && !os.IsNotExist(err) {
					failures = append(failures, err)
					continue
				}
				action = "removed"
			}
			results = append(results, map[string]string{"id": e.ID, "action": action})
		}
		if err := printResults(out, results, o.json); err != nil {
			return err
		}
		return errors.Join(failures...)
	case "logs", "open":
		entries, err := s.readAll()
		if err != nil {
			return err
		}
		entries, err = selectEntries(entries, o)
		if err != nil {
			return err
		}
		if len(entries) != 1 {
			return fmt.Errorf("select exactly one preview; matched %d", len(entries))
		}
		e := refresh(entries[0])
		if o.action == "open" {
			if !active(e.Status) {
				return fmt.Errorf("preview is %s", e.Status)
			}
			if err := openBrowser(e.URL); err != nil {
				return err
			}
			_, err = fmt.Fprintln(out, e.URL)
			return err
		}
		return printLogs(out, e, o.tail, o.json)
	case "doctor":
		port, err := allocatePort(o.portStart, o.portEnd, nil)
		if err != nil {
			return err
		}
		info := map[string]any{"version": version, "os": runtime.GOOS, "arch": runtime.GOARCH, "stateDir": s.root, "sampleFreePort": port, "shell": shellName()}
		return json.NewEncoder(out).Encode(info)
	default:
		return fmt.Errorf("unknown command %q; run previewctl help", o.action)
	}
}

func selectEntries(entries []*entry, o options) ([]*entry, error) {
	selectors := 0
	if o.id != "" {
		selectors++
	}
	if o.name != "" {
		selectors++
	}
	if o.all {
		selectors++
	}
	if selectors != 1 {
		return nil, errors.New("specify exactly one of --id, --name, or --all")
	}
	var selected []*entry
	for _, e := range entries {
		if o.all || o.id == e.ID || (o.name != "" && o.name == e.Name) {
			selected = append(selected, e)
		}
	}
	if len(selected) == 0 && !o.all {
		return nil, errors.New("no matching previews")
	}
	return selected, nil
}

func printEntries(out io.Writer, entries []*entry, asJSON bool) error {
	rows := make([]entry, 0, len(entries))
	for _, e := range entries {
		copy := *e
		copy.Token = ""
		copy.Control = ""
		rows = append(rows, copy)
	}
	if asJSON {
		return json.NewEncoder(out).Encode(rows)
	}
	if len(rows) == 0 {
		_, err := fmt.Fprintln(out, "No previews.")
		return err
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSTATUS\tURL\tEXPIRES")
	for _, e := range rows {
		expiry := "pinned"
		if e.ExpiresAt != nil {
			expiry = e.ExpiresAt.Local().Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.ID, e.Name, e.Status, e.URL, expiry)
	}
	return w.Flush()
}

func printResults(out io.Writer, rows []map[string]string, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(out).Encode(rows)
	}
	if len(rows) == 0 {
		_, err := fmt.Fprintln(out, "Nothing to clean.")
		return err
	}
	for _, r := range rows {
		if _, err := fmt.Fprintf(out, "%s: %s %s\n", r["id"], r["action"], r["reason"]); err != nil {
			return err
		}
	}
	return nil
}

func printLogs(out io.Writer, e *entry, n int, asJSON bool) error {
	readTail := func(path string) (string, error) {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return "", err
		}
		start := info.Size() - 1024*1024
		if start < 0 {
			start = 0
		}
		if _, err = f.Seek(start, io.SeekStart); err != nil {
			return "", err
		}
		b, err := io.ReadAll(f)
		if err != nil {
			return "", err
		}
		lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
		if start > 0 && len(lines) > 0 {
			lines = lines[1:]
		}
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		return strings.Join(lines, "\n"), nil
	}
	stdout, err := readTail(e.StdoutLog)
	if err != nil {
		return err
	}
	stderr, err := readTail(e.StderrLog)
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(out).Encode(map[string]string{"id": e.ID, "stdout": stdout, "stderr": stderr, "stdoutLog": e.StdoutLog, "stderrLog": e.StderrLog})
	}
	_, err = fmt.Fprintf(out, "stdout: %s\n%s\nstderr: %s\n%s\n", filepath.Clean(e.StdoutLog), stdout, filepath.Clean(e.StderrLog), stderr)
	return err
}

const helpText = `previewctl — local preview manager

Usage:
  previewctl start --name <name> --run '<command>' [options]
  previewctl list [--all] [--json]
  previewctl stop (--id <id> | --name <name> | --all) [--json]
  previewctl cleanup [--json]
  previewctl logs (--id <id> | --name <name>) [--tail <lines>] [--json]
  previewctl open (--id <id> | --name <name>)
  previewctl doctor
  previewctl version

Start options:
  --cwd <directory>       Working directory (default: current directory).
  --port <number>         Request a specific free port.
  --port-start <number>   First automatic port (default: 41000).
  --port-end <number>     Last automatic port (default: 41999).
  --ttl <duration>        Lifetime (default: 1h; units: s, m, h).
  --pinned               Disable expiry.
  --wait <duration>       Wait for a listening port (default: 10s).
  --grace <duration>      Unix shutdown grace period (default: 2s).
  --force                Restart the same name in the same directory.
  --json                 Return a JSON array.

All commands accept --state-dir <directory> or PREVIEWCTL_STATE_DIR.
The command uses {port}, {host}, {url}, or the PORT/HOST/PREVIEW_URL environment.
Windows runs commands with cmd.exe; macOS/Linux use /bin/sh.
Names are unique per working directory. Use an ID for an unambiguous target.
Legacy -Name, -Run, -Id, -Cwd, -Json and -TtlMinutes options are accepted.
Only a preview's authenticated supervisor can stop its managed processes.
`
