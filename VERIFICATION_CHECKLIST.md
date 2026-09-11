# Feature Verification Checklist

Use this checklist to verify autoreload behavior on your machine. Run from the project root unless noted.

---

## 1. Build and run

| Step | Command / action | Expected |
|------|------------------|----------|
| Build | `make build` | Exit 0; `bin/autoreload` exists. |

---

## 2. First build on start

| Step | Command / action | Expected |
|------|------------------|----------|
| Run demo | `make demo` | Logs: `[watcher] watching directories`, `[build] starting...`, `[build] ok`, `[server] started`, then test server line "Test server listening on :8080". No file change required. |

---

## 3. Server responds

| Step | Command / action | Expected |
|------|------------------|----------|
| With demo running | In another terminal: `curl -s http://localhost:8080/` | Response body contains "Hello!" and server time (e.g. RFC3339). |

---

## 4. Hot reload on .go change

| Step | Command / action | Expected |
|------|------------------|----------|
| With demo running | Edit `testserver/main.go` (e.g. change response text), save. | Logs: `[autoreload] change detected` (path=main.go), `[build] starting...`, `[build] ok`, `[autoreload] build completed`, `[server] started`, `[autoreload] server restarted`. |
| Verify | `curl -s http://localhost:8080/` | Response shows your new text. |

---

## 5. No rebuild on non-Go files

| Step | Command / action | Expected |
|------|------------------|----------|
| With demo running | `echo "x" >> testserver/README.md` or `touch testserver/foo.json` | No `[build] starting...` or `[autoreload] change detected`. Server keeps running. |

---

## 6. Debounce (one rebuild for many quick saves)

| Step | Command / action | Expected |
|------|------------------|----------|
| With demo running | Change and save `testserver/main.go` several times in 1–2 seconds. | One rebuild cycle only (one `[build] starting...` / `[build] ok` / `[autoreload] server restarted`). |

---

## 7. Build failure handling

| Step | Command / action | Expected |
|------|------------------|----------|
| With demo running | Introduce a syntax error in `testserver/main.go` (e.g. `x :=` with no value), save. | `[build] starting...`, `[build] failed` with duration and error; compiler output on stderr. Server is **not** restarted; autoreload keeps running. |
| Fix | Restore valid code, save. | Build succeeds and server restarts. |

---

## 8. Graceful shutdown

| Step | Command / action | Expected |
|------|------------------|----------|
| With demo running | Press Ctrl+C in the autoreload terminal. | Log: `[autoreload] shutting down...`. Process exits; no panic or hang. |

---

## 9. Manual restart hotkey

| Step | Command / action | Expected |
|------|------------------|----------|
| With demo running | Type `r` and press Enter (no file save). | Log: `[autoreload] manual restart requested`; then `[build] starting...`, `[build] ok`, `[autoreload] server restarted`. |

---

## 10. Config file support

| Step | Command / action | Expected |
|------|------------------|----------|
| Run with config only | `cd testserver && ../bin/autoreload` (no `--root`/`--build`/`--exec`). | Same as demo: build runs, server starts on :8080. Uses `testserver/autoreload.yaml`. |

---

## 11. CLI validation

| Step | Command / action | Expected |
|------|------------------|----------|
| No args | `./bin/autoreload` (from root; no config in cwd) | Stderr: usage message. Exit code non-zero. |
| Missing flags | `./bin/autoreload --root ./testserver` | Same: usage and non-zero exit. |

---

## 12. Invalid root handling

| Step | Command / action | Expected |
|------|------------------|----------|
| Bad root | `./bin/autoreload --root /nonexistent/path --build "true" --exec "true"` | Log: `[autoreload] failed to create watcher` with error (e.g. "no such file or directory"). Non-zero exit. |

---

## 13. Binary unchanged skip

| Step | Command / action | Expected |
|------|------------------|----------|
| With demo running | Trigger a rebuild that does not change the binary (e.g. add a comment in `testserver/main.go`, save; wait for restart; add another comment, save). | Second cycle may log `[autoreload] binary unchanged, skipping server restart` and no new `[server] started`. (Behavior can depend on timing.) |

---

## 14. Crash loop protection

| Step | Command / action | Expected |
|------|------------------|----------|
| Run with exiting exec | From project root: `./bin/autoreload --root ./testserver --build "go build -o ./bin/server ." --exec "exit 1"` (or from `testserver/`: `../bin/autoreload --root . --build "go build -o ./bin/server ." --exec "exit 1"`). | Build runs, server "starts" then exits. Log: `[server] crashed quickly, applying backoff` with `backoff=1s`. After ~1 s, one new build/restart. No repeated "[server] exited, restarting" lines; single restart per exit. |

---

## 15. Watcher health metrics

| Step | Command / action | Expected |
|------|------------------|----------|
| Start autoreload | `make demo` (or any valid run). | Startup logs include `[watcher] watching directories` with `count=N` and `[watcher] ignoring directories` with `count=M`. |

---

## 16. Structured logging

| Step | Command / action | Expected |
|------|------------------|----------|
| Run and trigger build | `make demo`, then save a change in `testserver/main.go`. | Log lines use prefixes: `[watcher]`, `[build]`, `[server]`, `[autoreload]`. |

---

## Quick reference

- **Demo (full flow):** `make build && make demo` → edit `testserver/main.go` → see one rebuild and restart.
- **Unit tests:** `go test ./internal/watcher/ ./internal/process/ -v`
