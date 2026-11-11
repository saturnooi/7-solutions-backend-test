# User Management API

A user management API in Go. The target design uses REST and gRPC, MongoDB, JWT (HS256), and hexagonal architecture (ports and adapters).

The project is built one feature at a time. Each feature must pass the full quality gate (`make check`) before the next one starts. The status table below shows what exists today.

## Status

| Phase | Scope | Status |
|---|---|---|
| 0 | Project foundation: tooling, lint rules, coverage gate, live reload | Done |
| 1 | User domain: entity, validation rules, domain errors | Done |
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

## User rules

The domain package [internal/domain/user](internal/domain/user) enforces these rules. It reports every invalid field at once, in the order name, email, password.

| Field | Normalization | Rules |
|---|---|---|
| `name` | Leading and trailing whitespace trimmed | Required. At most 100 characters (Unicode code points). No control characters such as line breaks. |
| `email` | Trimmed and lowercased | Required. At most 254 bytes. A bare address such as `alice@example.com`: no display name, comment, or quoted local part, and the domain must contain a dot. |
| `password` | None. It is never trimmed. | Required. At least 8 characters and at most 72 bytes. |
| Creation time | Converted to UTC and truncated to milliseconds | Must be set |

Error messages name the field and the rule, never the submitted value, for example `password must be at least 8 characters`.

## Project structure

```text
.
├── .air.toml                  # live-reload configuration for `make dev`
├── .golangci.yml              # linters and architecture rules
├── Makefile                   # development and quality-gate commands
├── go.mod
├── internal/
│   └── domain/
│       └── user/              # User entity, validation rules, domain errors
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

Tests use the standard `testing` package. Table-driven tests are the default style. Domain tests use real values only, because the domain has no dependencies to replace.

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
- **Validation is hand-written in the domain.** It uses only the standard library, so the core has no third-party dependency. The trade-off is a few more lines than a validator library would need.
- **Input is validated before the password is hashed.** `ValidateNewUser` checks the raw input, so invalid input never costs a bcrypt round. `New` validates the name and email again, so a `User` can never be built in an invalid state. The cost is that those two fields are checked twice.
- **Lengths are counted the way each limit needs.** Names and the password minimum count characters, so Thai text (3 bytes per character) is not cut short and three Thai letters do not pass as an 8-character password. The email maximum (RFC 5321) and the password maximum (bcrypt reads only 72 bytes) count bytes.
- **Emails are lowercased and must have a dotted domain.** Lowercasing the whole address makes the uniqueness check reliable. RFC 5321 allows a case-sensitive local part, but mainstream providers do not use one. A dotless domain such as `alice@gmail` is almost always a typo, so it is rejected. Intranet addresses such as `admin@localhost` are rejected for the same reason. Internationalized addresses (RFC 6531) are accepted.
- **Missing internal values are not validation errors.** The password hash and creation time come from the application layer, not the client. When one is missing, `New` returns a plain error, so it surfaces as an internal error instead of being blamed on the client's input.
- **Creation time is stored in UTC at millisecond precision.** That is the precision MongoDB keeps, so a stored user reads back exactly as it was created.
- **User IDs are opaque strings in the domain.** The repository assigns them when it stores a user, and the domain never sees a database ID type.
