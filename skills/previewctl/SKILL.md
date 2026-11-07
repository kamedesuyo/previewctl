---
name: previewctl
description: Start, inspect, and stop local web previews and development servers on Windows, macOS, and Linux through previewctl. Use for localhost previews, Vite or Next.js dev servers, static HTTP servers, preview URLs, logs, and cleanup; not for production deployment or remote hosting.
license: MIT
---

# previewctl

Manage local previews through the bundled CLI setup and `previewctl`. It allocates ports, records URLs and logs, and stops the managed process tree when its lifetime expires.

## Prepare the CLI

Resolve `<skill-directory>` to the absolute directory containing this `SKILL.md`. Do not assume a particular agent's installation path. Run the setup script for the host OS once per session and keep its returned executable path.

Windows PowerShell:

```powershell
$previewctl = & '<skill-directory>/scripts/setup.ps1'
& $previewctl version
& $previewctl list --json
```

macOS / Linux:

```sh
PREVIEWCTL="$(sh '<skill-directory>/scripts/setup.sh')"
"$PREVIEWCTL" version
"$PREVIEWCTL" list --json
```

Setup downloads the pinned release from `kamedesuyo/previewctl` on GitHub, checks its bundled SHA-256 values, and caches the executable outside the skill folder. Subsequent calls verify and reuse the cached binary without network access. It does not change PATH or require administrator access. Windows needs PowerShell 5.1 or later; macOS and Linux need `sh`, `curl`, `tar`, and `sha256sum` or `shasum`. No Go, Python, or Node.js runtime is required by the skill itself.

If setup fails, report the actual dependency, network, platform, or checksum error. Do not fall back to an unrelated `previewctl` on PATH: the older Windows-only edition uses a different state directory and command interface. The app being previewed still needs its own runtime and dependencies.

## Start and inspect

Use the resolved executable for every command below: `& $previewctl` in PowerShell or `"$PREVIEWCTL"` in a POSIX shell.

1. Run `list --json` before starting. Reuse a suitable preview for the same working directory and command.
2. Read the app's scripts or configuration to identify its foreground server command. Start it with the following arguments:

   ```text
   start --name <name> --cwd <absolute-project-directory> --run '<foreground-command>' --json
   ```

3. Configure that command to bind to `127.0.0.1` and consume `{port}` or the supplied `PORT` environment variable. `HOST` is `127.0.0.1`; `PREVIEW_URL` is the allocated URL. Quote the complete `--run` value as one argument. It executes under `cmd.exe` on Windows and `/bin/sh` on macOS/Linux, so use the target shell's quoting and variable syntax.
4. Retain the returned ID and URL. `running` means a TCP connection succeeded, not that the page or HTTP response is correct. Verify the returned URL using the available HTTP or browser tools. For `starting`, inspect logs and poll `list --json` within a bounded wait.
5. Give the user the returned URL and the relevant verification result. Use `open --id <id>` only when opening the system browser is useful or requested.

Do not launch the server directly, add `&`, use `nohup`, or daemonize it inside `--run`; previewctl owns background execution. Do not stop an unrelated process to free a port. Let previewctl allocate another port or choose an explicitly requested free port.

| Arguments | Use |
| --- | --- |
| `list --json` | Inspect active previews and unreachable supervisors. |
| `list --all --json` | Include stopped previews. |
| `logs --id <id> --tail <lines> --json` | Read the end of stdout/stderr and their paths. |
| `stop --id <id> --json` | Stop one managed preview and its children. |
| `cleanup --json` | Stop expired previews and remove stopped registrations. |
| `doctor` | Inspect OS, CPU, shell, state location, and port availability. |

Start options: `--ttl <duration>` defaults to `1h`; `--wait <duration>` defaults to `10s`. Durations use `s`, `m`, or `h`. Use `--pinned` only when the user wants the preview to persist without expiry. `--force` stops and replaces a matching preview; use it only when the requested change needs a restart. `--port <port>` selects a particular free port.

## Finish and troubleshoot

- Stop previews created only for smoke validation by their recorded IDs. Keep previews intended for the user to inspect, and state their expiry or pinned status.
- Read any needed logs before `cleanup --json`, since cleanup removes stopped registrations. Log files remain on disk.
- Do not use `stop --all` or a name matching several entries unless the user requested that scope. Do not kill by port or by a recorded PID.
- If a supervisor is `unreachable`, inspect logs and report the condition. Cleanup intentionally retains that registration; do not infer that the process is safe to kill or the record safe to delete.
- `--state-dir <directory>` or `PREVIEWCTL_STATE_DIR` changes the managed state location. Keep the same setting for start, list, logs, stop, and cleanup. State files contain control credentials; keep them out of repositories and shared directories.

For less common flags, run `help` on the resolved executable or read the [CLI usage](https://github.com/kamedesuyo/previewctl#起動).
