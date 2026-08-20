# Shared Logging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the crawler's local Logrus JSON formatter with the shared `don-register-common/logging` implementation while preserving the approved `oss-register` JSON schema and logging behavior.

**Architecture:** Pin `don-register-common` to PR 15 commit `980ff32ca88bcbd15be0fd66bb5be21f1a54ef50`, configure its `NewJSONLogger` as the process-wide `slog` logger, and keep a narrow local event adapter for the crawler's component and operation fields. Tests capture newline-delimited JSON from `slog` instead of Logrus hooks, so they validate the externally visible contract.

**Tech Stack:** Go 1.26.5, standard-library `log/slog`, `github.com/developer-overheid-nl/don-register-common/logging`, Testify, Changie.

**Spec:** User-approved design in this task and [don-register-common PR 15](https://github.com/developer-overheid-nl/don-register-common/pull/15).

## Global Constraints

- Every application log line is one JSON object on stdout.
- Every application log line contains `app: "oss-register"`.
- Crawler events contain `component` and `operation`.
- Supported levels remain `debug`, `info`, `warn`, and `error`; `trace` stays rejected.
- Optional file logging continues to duplicate the same JSON line to the configured file.
- The existing `.changes/unreleased/fix-logging-levels.yaml` remains the single Changie fragment.
- The dependency is pinned to exact commit `980ff32ca88bcbd15be0fd66bb5be21f1a54ef50` until the common package is released.

---

### Task 1: Lock the shared logger contract

**Files:**
- Modify: `main_test.go`
- Modify: `main.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: `commonlogging.NewJSONLogger(output io.Writer, appName, configuredLevel string) (*slog.Logger, error)` from PR 15.
- Produces: `configureLogging(console io.Writer) (io.Closer, error)` installs a shared JSON logger through `slog.SetDefault`.

- [x] **Step 1: Write the failing shared-logger test**

Replace the main-package test's Logrus state preservation and emissions with `slog.Default()`. In `TestConfigureLoggingWritesStructuredApplicationContext`, emit:

```go
slog.Default().Info(
	"Crawler run completed",
	"component", "crawler",
	"operation", "complete",
)
```

Keep assertions for `time`, `level`, `msg`, `app`, `component`, and `operation`. Convert the default-level, configured-level, error, and console/file tests to the equivalent `slog.Debug`, `slog.Info`, `slog.Warn`, and `slog.Error` calls. Rename the fallback-context test to assert explicitly supplied context, because the common constructor guarantees `app` while crawler events supply component and operation.

- [x] **Step 2: Run the focused test and verify RED**

Run: `go test . -run TestConfigureLoggingWritesStructuredApplicationContext -count=1`

Expected: FAIL because the current `configureLogging` only configures Logrus and does not route `slog.Default()` into the capture buffer.

- [x] **Step 3: Pin the common package and Go toolchain**

Run:

```bash
go get github.com/developer-overheid-nl/don-register-common@980ff32ca88bcbd15be0fd66bb5be21f1a54ef50
```

Confirm `go.mod` uses Go 1.26.5 and contains the generated pseudo-version resolving to the approved commit. Do not tidy until `main.go` imports the package, otherwise Go will correctly remove the still-unused dependency.

- [x] **Step 4: Configure the shared logger**

Replace Logrus setup in `main.go` with:

```go
logger, err := commonlogging.NewJSONLogger(console, "oss-register", viper.GetString("LOG_LEVEL"))
if err != nil {
	return nil, err
}
slog.SetDefault(logger)
```

For enabled file logging, create a second shared logger over `io.MultiWriter(console, file)` and set it as default. Keep file-open failures wrapped with the path.

- [x] **Step 5: Normalize modules and run the main-package tests GREEN**

Run:

```bash
go mod tidy
go test . -count=1
```

Expected: PASS for default filtering, structured fields, invalid levels, error serialization, and console/file duplication.

### Task 2: Replace the local formatter with a `slog` event adapter

**Files:**
- Delete: `internal/logging/formatter.go`
- Delete: `internal/logging/formatter_test.go`
- Create: `internal/logging/event.go`
- Create: `internal/logging/event_test.go`
- Modify: `apiclient/apiclient.go`

**Interfaces:**
- Consumes: the process-wide shared logger installed with `slog.SetDefault`.
- Produces: `Event(component, operation string) *Entry`, where `Entry` supports the crawler's existing `WithField`, `WithFields`, `WithError`, level, formatted-level, and fatal calls while delegating emission to `slog`.

- [x] **Step 1: Write failing adapter tests**

Replace formatter-specific level-normalization tests with behavioral tests that install a shared logger, call:

```go
Event("crawler", "complete").WithField("repository", "owner/repo").Info("Crawler run completed")
```

and assert the JSON event contains the shared `app` field plus component, operation, and repository context. Add a formatted warning assertion for the existing `Warnf` behavior.

- [x] **Step 2: Run adapter tests and verify RED**

Run: `go test ./internal/logging -count=1`

Expected: FAIL because `Event` still returns a Logrus entry and the local formatter remains responsible for JSON.

- [x] **Step 3: Implement the narrow adapter**

Implement `Entry` around `*slog.Logger`:

```go
type Entry struct {
	logger *slog.Logger
}

func Event(component, operation string) *Entry {
	return &Entry{logger: slog.Default().With(
		"component", component,
		"operation", operation,
	)}
}
```

Make field methods return new entries, format methods call `fmt.Sprintf`, and `Fatal` emits at `ERROR` before `os.Exit(1)` to preserve command behavior. Convert the four `log.Fields` values in `apiclient/apiclient.go` to `map[string]any`.

- [x] **Step 4: Remove Logrus and verify adapter tests GREEN**

Run:

```bash
go mod tidy
go test ./internal/logging -count=1
```

Expected: PASS and no direct or transitive source import of `github.com/sirupsen/logrus` in crawler code.

### Task 3: Migrate log-behavior tests to JSON events

**Files:**
- Create: `internal/loggingtest/recorder.go`
- Modify: `apiclient/logging_test.go`
- Modify: `crawler/logging_test.go`
- Modify: `scanner/logging_test.go`
- Modify: `git/repo_activity_logging_test.go`
- Modify: `main_test.go`

**Interfaces:**
- Consumes: the shared logger and local `Event` adapter.
- Produces: `loggingtest.Capture(t testing.TB, level string) *Recorder` and `(*Recorder).Events(t testing.TB) []map[string]any` for behavioral assertions.

- [x] **Step 1: Add the JSON recorder**

Implement a recorder that creates a common JSON logger over `bytes.Buffer`, saves and restores `slog.Default()`, and decodes all newline-delimited JSON events:

```go
func Capture(t testing.TB, level string) *Recorder {
	t.Helper()
	previous := slog.Default()
	logger, err := commonlogging.NewJSONLogger(&output, "oss-register", level)
	if err != nil {
		t.Fatal(err)
	}
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &Recorder{output: &output}
}
```

- [x] **Step 2: Convert package tests one package at a time**

Replace Logrus hook assertions with decoded JSON assertions. Compare level strings (`DEBUG`, `WARN`, `ERROR`), message strings from `msg`, and structured fields such as `repository`, `status_code`, and `error`. After each file, run its package test:

```bash
go test ./apiclient -count=1
go test ./crawler -count=1
go test ./scanner -count=1
go test ./git -count=1
```

Expected: each package passes before proceeding to the next.

- [x] **Step 3: Prove Logrus is gone**

Run: `rg -n 'sirupsen/logrus|logrus' --glob '*.go' --glob 'go.mod'`

Expected: no matches.

- [x] **Step 4: Run the complete test suite**

Run: `go test ./... -count=1`

Expected: PASS.

### Task 4: Document, format, and verify the migration

**Files:**
- Modify: `README.md`
- Modify: `.changes/unreleased/fix-logging-levels.yaml`
- Modify: all changed Go files through `gofmt`

**Interfaces:**
- Consumes: the completed shared logging integration.
- Produces: user-facing documentation and one release-note fragment describing the common logger pin.

- [x] **Step 1: Update documentation and the single Changie fragment**

Document that `don-register-common/logging` owns JSON formatting and that the temporary dependency pin targets PR 15 commit `980ff32`. Extend the existing fragment; do not create another fragment.

- [x] **Step 2: Format and normalize modules**

Run:

```bash
gofmt -w <all changed Go files>
go mod tidy
go mod verify
```

Expected: formatting succeeds and all module checks pass.

- [x] **Step 3: Run full verification from fresh output**

Run:

```bash
go test -race ./...
go vet ./...
go build ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --timeout=5m
changie next patch
```

Expected: all commands pass and `changie next patch` reports `v2.0.1`.

- [ ] **Step 4: Review exact staged scope, commit, and push**

Stage only the files named in this plan, inspect `git diff --cached`, commit once with:

```text
refactor(logging): use shared structured logger
```

Push `edit-logging` to `origin`, then confirm the local and remote commit IDs match and GitHub checks start successfully.
