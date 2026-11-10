# User Management API

A user management API in Go. The target design uses REST and gRPC, MongoDB, JWT (HS256), and hexagonal architecture (ports and adapters).

The project is built one feature at a time. Each feature must pass the full quality gate (`make check`) before the next one starts. The status table below shows what exists today.

## Status

| Phase | Scope | Status |
|---|---|---|
| 0 | Project foundation: tooling, lint rules, coverage gate, live reload | Done |
| 1 | User domain | Planned |
| 2 | User registration | Planned |
| 3 | Login and JWT issuing | Planned |
| 4 | JWT middleware | Planned |
| 5 | Get user by ID | Planned |
| 6 | List users | Planned |
| 7 | Update user | Planned |
| 8 | Delete user | Planned |
| 9 | HTTP logging middleware | Planned |
| 10 | Background user-count worker | Planned |
| 11 | gRPC (CreateUser, GetUser) | Planned |
| 12 | Docker and Docker Compose | Planned |
| 13 | Integration tests | Planned |
| 14 | Final quality gate | Planned |

## Architecture

Dependencies point inward: adapters depend on the application core, and the core never depends on adapters, frameworks, or drivers.

| Layer | Package | May import |
|---|---|---|
| Domain | `internal/domain/...` | Go standard library only |
| Application (use cases and ports) | `internal/application/...` | Domain and the standard library |
| Adapters (HTTP, gRPC, MongoDB, auth, worker) | `internal/adapters/...` | Application, domain, and their own drivers |
| Infrastructure (config, database connection) | `internal/infrastructure/...` | Whatever wiring needs |
| Composition root | `cmd/api` | Everything; builds and injects concrete types |

These rules are enforced by `depguard` in [.golangci.yml](.golangci.yml), so `make lint` fails on any violation. Specifically:

- `domain` may import only the standard library, and not `net/http` or `database/sql`.
- `application` may import only the standard library and `domain`.
- HTTP and gRPC adapters may not import the MongoDB driver or the repository adapter. They must go through use cases.

## Project structure

```text
.
├── .air.toml                  # live-reload configuration for `make dev`
├── .golangci.yml              # linters and architecture rules
├── Makefile                   # development and quality-gate commands
├── go.mod
└── scripts/
    └── coverage-check.sh      # per-layer coverage gate
```

Packages under `internal/` and `cmd/` are added as features are implemented. The Architecture section shows where each kind of code goes.

## Prerequisites

- Go 1.26 or newer
- [golangci-lint](https://golangci-lint.run/) v2
- make

## Development commands

| Command | What it does |
|---|---|
| `make dev` | Run the API with live reload ([Air](https://github.com/air-verse/air)). Needs `cmd/api`, which arrives in Phase 2. |
| `make fmt` | Format all Go files in place |
| `make fmt-check` | Fail if any file is not gofmt-formatted |
| `make build` | Compile every package |
| `make test` | Run unit tests |
| `make test-race` | Run unit tests with the race detector |
| `make test-coverage` | Write `coverage.out` and print per-function coverage |
| `make coverage-html` | Render `coverage.html` |
| `make coverage-check` | Enforce the coverage thresholds below |
| `make test-integration` | Run unit and integration tests (`-tags=integration`) |
| `make vet` | Run `go vet` |
| `make lint` | Run golangci-lint, including the architecture rules |
| `make check` | Full gate: fmt-check → build → test → test-race → coverage-check → vet → lint |

## Testing and coverage

Tests use the standard `testing` package. Table-driven tests are the default style. Dependencies are replaced with small hand-written fakes instead of a mocking library.

`make coverage-check` runs [scripts/coverage-check.sh](scripts/coverage-check.sh) on the unit-test profile and fails if any package is below its threshold:

| Scope | Minimum |
|---|---|
| `internal/domain/...` | 90% |
| `internal/application/...` | 90% |
| `internal/adapters/http/...` | 80% |
| `internal/adapters/grpc/...` | 80% |
| Overall | 80% |

These packages are excluded from the unit-test gate:

| Package | Reason |
|---|---|
| `cmd/...` | Composition root. It only wires dependencies and is exercised by integration and end-to-end runs. |
| `gen/...` | Generated protobuf code. |
| `internal/infrastructure/database` | Needs a real MongoDB, so it is covered by `make test-integration`. |
| `internal/adapters/repository/mongodb` | Needs a real MongoDB, so it is covered by `make test-integration`. |

Because of these exclusions, the gate's `OVERALL` figure can differ from the `total` printed by `go tool cover -func`, which counts every package.

## Design decisions

- **Ports live in the application layer.** Repository and service interfaces are declared next to the use cases that consume them, not in a separate `ports` package. This follows the Go convention of defining interfaces where they are used, and still keeps every dependency pointing into the core.
- **Architecture rules are checked by the linter.** `depguard` turns layer boundaries into lint errors, so a violation fails `make check`.
- **Coverage is gated per layer.** A single overall number can hide an untested use case, so each core layer has its own threshold.
- **Air is pinned and run with `go run`.** `make dev` runs `go run github.com/air-verse/air@v1.67.4`, so there is no global install and no version drift. Registering Air as a `tool` in `go.mod` would add 23 dependencies unrelated to the API.
- **Reloads shut down gracefully.** Air sends SIGINT before it restarts or exits (`send_interrupt`), and force-kills only after 5 seconds (`kill_delay`). It also loads `.env` when that file exists (`env_files`).
