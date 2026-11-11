# User Management API

A user management API in Go. The target design uses REST and gRPC, MongoDB, JWT (HS256), and hexagonal architecture (ports and adapters).

The project is built one feature at a time. Each feature must pass the full quality gate (`make check`) before the next one starts. The status table below shows what exists today.

## Status

| Phase | Scope | Status |
|---|---|---|
| 0 | Project foundation: tooling, lint rules, coverage gate, live reload | Done |
| 1 | User domain: entity, validation rules, domain errors | Done |
| 2 | User registration: use case, MongoDB repository, bcrypt, `POST /api/v1/auth/register` | Done |
| 3 | Login and JWT issuing: `POST /api/v1/auth/login`, HS256 access tokens | Done |
| 4 | JWT middleware and the protected `POST /api/v1/users` | Done |
| 5 | Get user by ID: `GET /api/v1/users/{id}` | Done |
| 6 | List users: `GET /api/v1/users` with limit/offset pagination | Done |
| 7 | Update user: `PATCH /api/v1/users/{id}`, own account only | Done |
| 8 | Delete user: `DELETE /api/v1/users/{id}`, own account only; tokens of deleted users stop working | Done |
| 9 | HTTP logging middleware: method, path, status, duration | Done |
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

How requests flow through the layers:

```text
POST /api/v1/auth/register
  → adapters/http            decode and limit the JSON body, map errors to status codes
  → application/userservice  validate, hash the password, build the User, store it
      ├─ PasswordHasher port → adapters/auth (bcrypt)
      └─ UserRepository port → adapters/repository/mongodb (unique email index)
  → domain/user              validation rules and normalization

POST /api/v1/auth/login
  → adapters/http            decode the body, map errors to status codes
  → application/userservice  normalize the email, find the user, compare the password, issue a token
      ├─ UserRepository port → adapters/repository/mongodb (FindByEmail)
      ├─ PasswordHasher port → adapters/auth (bcrypt)
      └─ TokenIssuer port    → adapters/auth (JWT HS256)

Every protected route
  → adapters/http            requireAuth: read the bearer token
  → application/userservice  Authenticator: verify the token, then check that its user still exists
      ├─ TokenVerifier port  → adapters/auth (JWT HS256)
      └─ UserFinder port     → adapters/repository/mongodb (FindByID)
  → adapters/http            put the user ID in the request context and call the handler

POST /api/v1/users
  → requireAuth, then the same Register use case as /api/v1/auth/register

GET /api/v1/users/{id}
  → adapters/http            requireAuth, then read {id} from the path
  → application/userservice  GetUser
      └─ UserRepository port → adapters/repository/mongodb (parse the ObjectID, then FindOne)

GET /api/v1/users?limit=&offset=
  → adapters/http            requireAuth, then parse limit and offset as integers (default limit 20)
  → application/userservice  ListUsers: check 1 ≤ limit ≤ 100 and offset ≥ 0
      └─ UserRepository port → adapters/repository/mongodb (Find sorted by the created_at_desc index)

PATCH /api/v1/users/{id}
  → adapters/http            requireAuth, decode {name?, email?}, take the caller's ID from the token
  → application/userservice  UpdateUser: caller must own the account (403), then build the changes
  → domain/user              NewChanges: normalize and validate only the fields that were sent
      └─ UserRepository port → adapters/repository/mongodb (FindOneAndUpdate with $set on those fields)

DELETE /api/v1/users/{id}
  → adapters/http            requireAuth, take the caller's ID from the token
  → application/userservice  DeleteUser: caller must own the account (403)
      └─ UserRepository port → adapters/repository/mongodb (DeleteOne; nothing deleted means 404)
```

## User rules

The domain package [internal/domain/user](internal/domain/user) enforces these rules. It reports every invalid field at once, in the order name, email, password.

| Field | Normalization | Rules |
|---|---|---|
| `name` | Leading and trailing whitespace trimmed | Required. At most 100 characters (Unicode code points). No control characters such as line breaks. |
| `email` | Trimmed and lowercased | Required. At most 254 bytes. A bare address such as `alice@example.com`: no display name, comment, or quoted local part, and the domain must contain a dot. Unique across users. |
| `password` | None. It is never trimmed. | Required. At least 8 characters and at most 72 bytes. Stored only as a bcrypt hash. |
| Creation time | Converted to UTC and truncated to milliseconds | Must be set |

Error messages name the field and the rule, never the submitted value, for example `password must be at least 8 characters`.

## Project structure

```text
.
├── .air.toml                  # live-reload configuration for `make dev`
├── .env.example               # environment variables for local runs
├── .golangci.yml              # linters and architecture rules
├── .mockery.yml               # mock generation for `make mocks`
├── Makefile                   # development and quality-gate commands
├── docker-compose.yml         # MongoDB for local runs and integration tests
├── go.mod
├── cmd/
│   └── api/                   # composition root: config, wiring, graceful shutdown
├── internal/
│   ├── domain/
│   │   └── user/              # User entity, validation rules, domain errors
│   ├── application/
│   │   └── userservice/       # use cases and the ports they need
│   ├── adapters/
│   │   ├── auth/              # bcrypt password hasher, JWT issuer
│   │   ├── http/              # REST handlers, DTOs, error mapping, auth and logging middleware
│   │   └── repository/
│   │       └── mongodb/       # UserRepository on MongoDB
│   └── infrastructure/
│       ├── config/            # environment variables
│       └── database/          # MongoDB connection
└── scripts/
    └── coverage-check.sh      # per-layer coverage gate
```

## Prerequisites

- Go 1.26 or newer
- Docker with Compose v2, for MongoDB
- [golangci-lint](https://golangci-lint.run/) v2
- make

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `HTTP_ADDR` | No | `:8080` | Address the HTTP server listens on |
| `MONGO_URI` | Yes | | MongoDB connection string |
| `MONGO_DATABASE` | No | `user_management` | Database name |
| `BCRYPT_COST` | No | `12` | bcrypt work factor, from 10 to 31 |
| `JWT_SECRET` | Yes | | HMAC key for signing tokens, at least 32 bytes |
| `JWT_ISSUER` | No | `user-management-api` | `iss` claim of issued tokens |
| `JWT_TTL` | No | `1h` | Token lifetime, a Go duration such as `15m` or `1h` |

Startup fails with a message listing every invalid variable. [.env.example](.env.example) holds working values for local development, except `JWT_SECRET`, which is left empty on purpose so no shared secret is ever used. Copy the file to `.env`, which is git-ignored and loaded by `make dev`.

Integration tests read `MONGO_TEST_URI` instead (default `mongodb://localhost:27017`), so they can never pick up the application's database.

## Running locally

```bash
make db-up
cp .env.example .env
echo "JWT_SECRET=$(openssl rand -base64 48)" >> .env
make dev
```

The `echo` line generates a random 64-character secret. Without it, startup stops with `config: JWT_SECRET is required and must be at least 32 bytes`.

`make db-up` starts MongoDB 8.0 on `127.0.0.1:27017` and waits until it is healthy. The API connects, creates the unique email index, and listens on `:8080`. Ctrl+C shuts it down gracefully. `make db-down` stops MongoDB and keeps its data in the `mongo-data` volume.

## REST API

All bodies are JSON. Errors share one shape:

```json
{"error":{"code":"<machine-readable code>","message":"<human-readable message>"}}
```

### POST /api/v1/auth/register

Auth: none

Creates a user. The request body may contain only these three fields and must not exceed 1 MiB.

Request:

```bash
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"name":"Alice Smith","email":"Alice@Example.com","password":"correct horse"}'
```

Response `201 Created`:

```json
{"id":"6af3ee86a4bf57df9bcdbf93","name":"Alice Smith","email":"alice@example.com","created_at":"2026-11-11T04:26:46.687Z"}
```

Response `400 Bad Request` for invalid fields:

```json
{"error":{"code":"validation_failed","message":"request validation failed","fields":[{"field":"name","message":"is required"},{"field":"email","message":"must be a valid email address"},{"field":"password","message":"must be at least 8 characters"}]}}
```

Response `409 Conflict` when the email is taken (compared after lowercasing):

```json
{"error":{"code":"email_already_exists","message":"email is already registered"}}
```

| Status | Code | When |
|---|---|---|
| 201 | | User created |
| 400 | `validation_failed` | A field breaks a [user rule](#user-rules) |
| 400 | `invalid_json` | Malformed JSON, unknown fields, wrong types, or more than one JSON value |
| 409 | `email_already_exists` | Another user has this email |
| 413 | `request_too_large` | Body larger than 1 MiB |
| 500 | `internal_error` | Unexpected failure. Details are logged, never returned. |

### POST /api/v1/auth/login

Auth: none

Exchanges an email and password for an access token. The email is trimmed and lowercased before the lookup, as at registration.

Request:

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"correct horse"}'
```

Response `200 OK`, with `Cache-Control: no-store`:

```json
{"access_token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJ1c2VyLW1hbmFnZW1lbnQtYXBpIiwic3ViIjoiNmFmM2ZjZjkxOWE5MzZlNDk1MzM4NmFmIiwiZXhwIjoxNzk0Mzc4NTA1LCJpYXQiOjE3OTQzNzQ5MDV9.<signature>","token_type":"Bearer","expires_in":3600}
```

Response `401 Unauthorized` for an unknown email or a wrong password. Both get the same body:

```json
{"error":{"code":"invalid_credentials","message":"invalid email or password"}}
```

| Status | Code | When |
|---|---|---|
| 200 | | Credentials are valid |
| 400 | `validation_failed` | `email` or `password` is empty |
| 400 | `invalid_json` | Malformed JSON, unknown fields, wrong types, or more than one JSON value |
| 401 | `invalid_credentials` | Unknown email or wrong password |
| 413 | `request_too_large` | Body larger than 1 MiB |
| 500 | `internal_error` | Unexpected failure. Details are logged, never returned. |

### POST /api/v1/users

Auth: `Authorization: Bearer <access_token>`

Creates a user on behalf of an authenticated caller. The body, rules and responses are the same as for [registration](#post-apiv1authregister); only the token requirement differs. Any authenticated user may call it.

Request:

```bash
curl -X POST http://localhost:8080/api/v1/users \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Bob Jones","email":"bob@example.com","password":"correct horse"}'
```

Response `201 Created`:

```json
{"id":"6af400dac46deb7de7f3b26f","name":"Bob Jones","email":"bob@example.com","created_at":"2026-11-11T05:44:58.345Z"}
```

Response `401 Unauthorized` when the token is missing or rejected (see [Using the token](#using-the-token)).

### GET /api/v1/users/{id}

Auth: `Authorization: Bearer <access_token>`

Returns one user. Any authenticated user may read any user.

Request:

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/users/6af4028b59331b9de9b10201
```

Response `200 OK`:

```json
{"id":"6af4028b59331b9de9b10201","name":"Alice Smith","email":"alice@example.com","created_at":"2026-11-11T05:52:11.414Z"}
```

Response `404 Not Found` for a well-formed ID that matches no user:

```json
{"error":{"code":"user_not_found","message":"user not found"}}
```

Response `400 Bad Request` when the ID is not a MongoDB ObjectID:

```json
{"error":{"code":"invalid_id","message":"id must be a 24-character hexadecimal string"}}
```

| Status | Code | When |
|---|---|---|
| 200 | | User found |
| 400 | `invalid_id` | The ID is not 24 hexadecimal characters |
| 401 | `unauthorized` | Token missing or rejected |
| 404 | `user_not_found` | No user has this ID |
| 500 | `internal_error` | Unexpected failure. Details are logged, never returned. |

### GET /api/v1/users

Auth: `Authorization: Bearer <access_token>`

Lists users, newest first. Users created in the same millisecond are ordered by ID, so pages never overlap or skip anyone because of a tie.

| Query parameter | Default | Allowed |
|---|---|---|
| `limit` | `20` | 1 to 100 |
| `offset` | `0` | 0 or more |

Request:

```bash
curl -H "Authorization: Bearer $TOKEN" 'http://localhost:8080/api/v1/users?limit=2&offset=1'
```

Response `200 OK`:

```json
{"users":[{"id":"6af40496bc5a249ffce49e01","name":"Bob Jones","email":"bob@example.com","created_at":"2026-11-11T06:00:54.888Z"},{"id":"6af40496bc5a249ffce49e00","name":"Alice Smith","email":"alice@example.com","created_at":"2026-11-11T06:00:54.657Z"}],"limit":2,"offset":1}
```

A page past the end is an empty array, never `null`:

```json
{"users":[],"limit":20,"offset":10}
```

A client has reached the end when a page holds fewer than `limit` users.

Response `400 Bad Request` for values outside the allowed range:

```json
{"error":{"code":"validation_failed","message":"request validation failed","fields":[{"field":"limit","message":"must be between 1 and 100"},{"field":"offset","message":"must not be negative"}]}}
```

| Status | Code | When |
|---|---|---|
| 200 | | A page of users, possibly empty |
| 400 | `validation_failed` | `limit` or `offset` is not an integer or is out of range |
| 401 | `unauthorized` | Token missing or rejected |
| 500 | `internal_error` | Unexpected failure. Details are logged, never returned. |

### PATCH /api/v1/users/{id}

Auth: `Authorization: Bearer <access_token>`, and `{id}` must be the caller's own ID (the token's `sub`).

Changes the caller's name, email, or both. A field that is absent or `null` is left unchanged. Values follow the same [user rules](#user-rules) as registration: the name is trimmed, and the email is trimmed, lowercased, and must not belong to another user. The password cannot be changed here; sending `password` is rejected as an unknown field.

Request:

```bash
curl -X PATCH http://localhost:8080/api/v1/users/6af409a28ce8e42353cce49a \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"email":" Alice.Cooper@Example.com "}'
```

Response `200 OK`, with the updated user:

```json
{"id":"6af409a28ce8e42353cce49a","name":"Alice Cooper","email":"alice.cooper@example.com","created_at":"2026-11-11T06:22:26.129Z"}
```

Response `403 Forbidden` when `{id}` is another user:

```json
{"error":{"code":"forbidden","message":"you may only change your own account"}}
```

Response `400 Bad Request` for an empty update such as `{}`:

```json
{"error":{"code":"empty_update","message":"request must change name or email"}}
```

| Status | Code | When |
|---|---|---|
| 200 | | Updated |
| 400 | `validation_failed` | A sent field breaks a [user rule](#user-rules) |
| 400 | `empty_update` | Neither `name` nor `email` was sent |
| 400 | `invalid_json` | Malformed JSON or an unknown field such as `password` |
| 401 | `unauthorized` | Token missing or rejected |
| 403 | `forbidden` | `{id}` is not the caller's own ID |
| 404 | `user_not_found` | The caller's account no longer exists |
| 409 | `email_already_exists` | Another user has this email |
| 413 | `request_too_large` | Body larger than 1 MiB |
| 500 | `internal_error` | Unexpected failure. Details are logged, never returned. |

Because the token's `sub` is the user ID, not the email, a token stays valid after its owner changes email. The next login uses the new email.

### DELETE /api/v1/users/{id}

Auth: `Authorization: Bearer <access_token>`, and `{id}` must be the caller's own ID (the token's `sub`).

Deletes the caller's account. Every token issued to it stops working at once, because each protected request checks that the token's user still exists. The email becomes free to register again, and a new account gets a new ID, so old tokens never apply to it.

Request:

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/users/6af40e79c3bde32d45044878
```

Response `204 No Content`, with an empty body.

Response `403 Forbidden` when `{id}` is another user:

```json
{"error":{"code":"forbidden","message":"you may only change your own account"}}
```

After the delete, the same token gets `401`:

```json
{"error":{"code":"unauthorized","message":"a valid bearer token is required"}}
```

| Status | Code | When |
|---|---|---|
| 204 | | Deleted |
| 401 | `unauthorized` | Token missing or rejected, including a token whose user is already deleted |
| 403 | `forbidden` | `{id}` is not the caller's own ID |
| 500 | `internal_error` | Unexpected failure. Details are logged, never returned. |

## JWT

Login returns an access token signed with HMAC-SHA256 (HS256) using `JWT_SECRET`. Decoded, the token from the example above is:

```json
{"alg":"HS256","typ":"JWT"}
{"iss":"user-management-api","sub":"6af3fcf919a936e4953386af","exp":1794378505,"iat":1794374905}
```

| Claim | Meaning |
|---|---|
| `sub` | ID of the user who logged in |
| `iss` | `JWT_ISSUER` |
| `iat` | When the token was issued (Unix seconds) |
| `exp` | When the token expires: `iat` plus `JWT_TTL` |

`expires_in` in the login response is the token lifetime in seconds. There are no refresh tokens, so a client logs in again after the token expires.

### Using the token

1. Register: `POST /api/v1/auth/register`.
2. Log in: `POST /api/v1/auth/login`, and keep `access_token`.
3. Send it with every protected request. The example uses [jq](https://jqlang.org/) to extract the token:

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"correct horse"}' | jq -r .access_token)

curl -H "Authorization: Bearer $TOKEN" ...
```

The scheme name is case-insensitive (`Bearer` or `bearer`). A token is accepted only if all of these hold:

| Check | Rejected example |
|---|---|
| Signed with HS256 and `JWT_SECRET` | `alg: none`, HS512, another secret, or a payload edited after signing |
| `exp` is present and in the future | Expired, or no `exp` claim |
| `iat` is not in the future | Issued by a clock that runs ahead |
| `iss` equals `JWT_ISSUER` | Token from another issuer |
| `sub` is not empty | Token without a subject |
| `sub` is the ID of an existing user | Token of a deleted account |

`exp` and `iat` allow 5 seconds of clock difference between servers. The last check costs one lookup by `_id` per request.

Every rejection returns the same `401` body, so a client cannot learn why a token failed:

```json
{"error":{"code":"unauthorized","message":"a valid bearer token is required"}}
```

The `WWW-Authenticate` header follows RFC 6750: `Bearer` when no token was sent, and `Bearer error="invalid_token"` when a token was sent but rejected. If the user lookup itself fails, for example while MongoDB is unreachable, the response is `500 internal_error` rather than `401`, so an outage does not look like every client being logged out. On success, the middleware stores the token's `sub` (the user ID) in the request context for the handler.

## Logging

The API writes JSON lines to standard output with `log/slog`. A middleware around the whole router logs every request once, after the response, including `401`, `404` and `405` responses. Lines from a real run:

```json
{"time":"2026-11-11T13:55:29.938724+07:00","level":"INFO","msg":"http request","method":"POST","path":"/api/v1/auth/register","status":201,"duration_ms":228.198}
{"time":"2026-11-11T13:55:30.287096+07:00","level":"INFO","msg":"http request","method":"GET","path":"/api/v1/users","status":200,"duration_ms":8.406}
{"time":"2026-11-11T13:55:30.297641+07:00","level":"INFO","msg":"http request","method":"GET","path":"/api/v1/users","status":401,"duration_ms":0.068}
{"time":"2026-11-11T13:55:30.321244+07:00","level":"INFO","msg":"http request","method":"GET","path":"/nowhere","status":404,"duration_ms":0.01}
```

| Field | Meaning |
|---|---|
| `method` | HTTP method |
| `path` | URL path only, without the query string |
| `status` | Status code sent to the client. It is `200` when a handler writes a body without setting one, as `net/http` does. |
| `duration_ms` | Time from receiving the request to finishing the response, in milliseconds |

Request logs never include headers (so never the `Authorization` token), request or response bodies (so never passwords or emails), or the query string. The second line above came from `GET /api/v1/users?limit=5&offset=0` with a bearer token, and neither appears in it.

When a request fails with `500`, a separate `ERROR` line named `request failed` records the real cause. The client only ever sees `internal_error`.

## Development commands

| Command | What it does |
|---|---|
| `make dev` | Run the API with live reload ([Air](https://github.com/air-verse/air)) |
| `make db-up` | Start MongoDB in Docker and wait until it is healthy |
| `make db-down` | Stop MongoDB, keeping its data |
| `make mocks` | Regenerate test mocks from [.mockery.yml](.mockery.yml) |
| `make fmt` | Format all Go files in place |
| `make fmt-check` | Fail if any file is not gofmt-formatted |
| `make build` | Compile every package |
| `make test` | Run unit tests |
| `make test-race` | Run unit tests with the race detector |
| `make test-coverage` | Write `coverage.out` and print per-function coverage |
| `make coverage-html` | Render `coverage.html` |
| `make coverage-check` | Enforce the coverage thresholds below |
| `make test-integration` | Run unit and integration tests (`-tags=integration`). Needs `make db-up`. |
| `make vet` | Run `go vet`, including integration test files |
| `make lint` | Run golangci-lint, including the architecture rules and integration test files |
| `make check` | Full gate: fmt-check → build → test → test-race → coverage-check → vet → lint |

## Testing and coverage

Tests use the standard `testing` package. Table-driven tests are the default style.

| Layer | How it is tested |
|---|---|
| Domain | Real values only. The domain has no dependencies. |
| Application | Ports replaced by generated mocks |
| HTTP | `httptest` through the real router, with the use case replaced by a generated mock. Bodies are compared exactly, so a leaked field fails the test. |
| Logging middleware | Status capture with small stub handlers, the full router for wiring, and `testing/synctest` for the duration: a handler that sleeps 150 ms on the fake clock must log exactly `150`, with no real waiting |
| Authentication | The `Authenticator` use case with mocked ports; header parsing with a mocked `Authenticator`; and end-to-end with the real JWT service and `Authenticator`: a valid token, an expired one, a foreign-secret one, and a valid token whose user was deleted |
| bcrypt hasher, JWT issuer, config | Real implementations. Issued tokens are parsed back and their claims checked. |
| MongoDB repository | Integration tests against a real MongoDB. Each test uses its own database and drops it afterwards. |

Mocks are generated by [mockery](https://vektra.github.io/mockery/) v3.9.1 with the `matryer` template. Each mock is a plain struct with a function field per method that records its calls, and it imports only the standard library. Tests never use testify or gomock. The mocks live next to their interface as `mocks_test.go`, so they compile only for tests. Run `make mocks` after changing a mocked interface, and commit the result with the interface.

To run the integration tests:

```bash
make db-up
make test-integration
```

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
- **Package names avoid collisions.** The use cases live in `userservice`, so they never clash with the domain package `user`. The HTTP adapter is package `http` inside `internal/adapters/http`, the same pattern go-kit uses. Only `cmd/api` needs an import alias.
- **Architecture rules are checked by the linter.** `depguard` turns layer boundaries into lint errors, so a violation fails `make check`.
- **Coverage is gated per layer.** A single overall number can hide an untested use case, so each core layer has its own threshold.
- **Air is pinned and run with `go run`.** `make dev` runs `go run github.com/air-verse/air@v1.67.4`, so there is no global install and no version drift. Registering Air as a `tool` in `go.mod` would add 23 dependencies unrelated to the API. mockery is pinned the same way.
- **Reloads shut down gracefully.** Air sends SIGINT before it restarts or exits (`send_interrupt`), and force-kills only after 5 seconds (`kill_delay`). It also loads `.env` when that file exists (`env_files`).
- **Validation is hand-written in the domain.** It uses only the standard library, so the core has no third-party dependency. The trade-off is a few more lines than a validator library would need.
- **Input is validated before the password is hashed.** `ValidateNewUser` checks the raw input, so invalid input never costs a bcrypt round. `New` validates the name and email again, so a `User` can never be built in an invalid state. The cost is that those two fields are checked twice.
- **Lengths are counted the way each limit needs.** Names and the password minimum count characters, so Thai text (3 bytes per character) is not cut short and three Thai letters do not pass as an 8-character password. The email maximum (RFC 5321) and the password maximum (bcrypt reads only 72 bytes) count bytes.
- **Emails are lowercased and must have a dotted domain.** Lowercasing the whole address makes the uniqueness check reliable. RFC 5321 allows a case-sensitive local part, but mainstream providers do not use one. A dotless domain such as `alice@gmail` is almost always a typo, so it is rejected. Intranet addresses such as `admin@localhost` are rejected for the same reason. Internationalized addresses (RFC 6531) are accepted.
- **Email uniqueness is enforced by a unique index.** The repository inserts and maps MongoDB's duplicate-key error to `ErrEmailAlreadyExists`. Checking for an existing email first would leave a race between two concurrent registrations.
- **Request bodies are decoded strictly.** Unknown fields are rejected, which blocks mass-assignment attempts such as `"role":"admin"`. The body must be one JSON value and at most 1 MiB.
- **Internal errors never reach the client.** Unexpected errors are logged with the method and path, and the client receives only `internal_error`. Passwords and hashes are never logged, and responses never contain a hash.
- **bcrypt cost defaults to 12.** Registration then takes about 230 ms on a laptop. `BCRYPT_COST` accepts 10 to 31, where 10 is the common minimum recommendation.
- **Every MongoDB call has a timeout.** The client is created with a 5-second operation timeout, so a call without its own deadline cannot hang a request.
- **Login failures look the same.** An unknown email and a wrong password both return `401 invalid_credentials`. For an unknown email, the service still runs a bcrypt comparison against a dummy hash, so the response time does not reveal which emails are registered. In a local run, both cases took about 0.22 s.
- **Passwords over 72 bytes never match.** bcrypt's `CompareHashAndPassword` reads only the first 72 bytes, so a user with a 72-byte password could otherwise log in with that password plus any suffix. The hasher's `Compare` rejects longer passwords before calling bcrypt.
- **The JWT secret has no default.** `JWT_SECRET` must be set and at least 32 bytes long, and `.env.example` leaves it empty. The JWT adapter also refuses a shorter key itself, because golang-jwt signs with an empty key without complaint.
- **Only HS256 is issued, with minimal claims.** Tokens carry `sub`, `iss`, `iat` and `exp`, and nothing personal such as the email, because a JWT payload is readable by anyone who holds the token.
- **Request logging wraps the whole router.** It sits outside routing and authentication, so it also records requests that never reach a handler: unknown paths, wrong methods, rejected tokens. Request lines are always `INFO`. The cause of a `500` is logged once at `ERROR` where it happens, not repeated by the request logger.
- **The status recorder keeps the writer's other features.** The wrapper records the first status, or `200` once a body is written, matching what `net/http` sends. It implements `Unwrap`, so `http.ResponseController` can still reach the real writer (for example to flush).
- **Authentication is a use case.** A token counts only while its user exists, which is a business rule, not just a JWT check. `userservice.Authenticator` verifies the token through the `TokenVerifier` port (the JWT adapter) and looks the user up through the narrow `UserFinder` port. The HTTP middleware calls it, and the gRPC interceptor will too, so the rule exists once. The JWT adapter still owns every signature and claim check.
- **Deleting an account revokes its tokens immediately.** The existence check costs one `_id` lookup per protected request, which keeps tokens stateless while closing the window a deleted user would otherwise have until `exp`. A failed lookup is a `500`, never a `401`, so a database outage does not log everyone out.
- **The middleware lives in the HTTP adapter package.** A separate package would have to export or copy the JSON error helpers to return the same error shape.
- **Rejections do not say why.** Missing, malformed, expired and forged tokens all get the same `401` body. Only the RFC 6750 `WWW-Authenticate` header separates "no token" from "bad token", which the client already knows.
- **Clock leeway is 5 seconds.** That absorbs small clock differences between replicas without meaningfully extending a token's life.
- **Token responses are not cached.** The login response sends `Cache-Control: no-store`, as RFC 6749 requires for token responses.
- **Login validates presence only.** An empty email or password is a `400`. Any other mismatch, including a malformed email, is a `401`, so login does not reveal the validation rules of stored accounts.
- **Missing internal values are not validation errors.** The password hash and creation time come from the application layer, not the client. When one is missing, `New` returns a plain error, so it surfaces as an internal error instead of being blamed on the client's input.
- **Creation time is stored in UTC at millisecond precision.** That is the precision MongoDB keeps, so a stored user reads back exactly as it was created.
- **User IDs are opaque strings in the domain.** The repository generates ObjectIDs when it stores a user, and only the MongoDB adapter knows that type.
- **Users may change only their own account, and the rule lives in the use case.** `UpdateUser` takes the caller's ID from the token, so HTTP and a future gRPC method enforce the same rule. Ownership is checked before anything else, so a non-owner gets `403` whether or not the target exists and cannot use the endpoint to probe for IDs.
- **Updates touch only the fields that were sent.** The domain builds a `Changes` value with only those fields, already normalized and validated. The repository applies it with one `FindOneAndUpdate` and `$set`, without reading the user first, so two concurrent updates to different fields cannot overwrite each other. The same unique email index that guards registration turns a taken email into `409`.
- **The password is not part of the update.** Changing it should require the current password, which the coding test does not ask for. Sending `password` is rejected rather than silently ignored.
- **The page-size cap lives in the use case.** HTTP only turns the query string into integers. The default of 20 and the rule 1 ≤ `limit` ≤ 100 are in `ListUsers`, because the cap protects the database no matter which adapter calls it.
- **Listing is backed by an index.** `EnsureIndexes` creates `created_at_desc` on `{created_at: -1, _id: -1}`, which matches the sort exactly. `explain()` on a real run shows an `IXSCAN` on that index, so MongoDB never sorts in memory as the collection grows.
- **Offset pagination over cursors.** `limit` and `offset` are simple to use and to test, and enough for an admin-style list. See Known limitations for the trade-off.
- **The ID format is checked in the repository adapter, before any query.** Only the MongoDB adapter knows IDs are ObjectIDs, so it parses the ID and returns `ErrInvalidID` without touching the database. The integration tests prove this: a malformed ID returns `ErrInvalidID` even when the context is already cancelled. HTTP maps it to `400 invalid_id`, and a well-formed ID that matches nothing to `404 user_not_found`, so a client can tell a typo from a missing user.

## Known limitations

- The local MongoDB container has no authentication. It is published only on `127.0.0.1`.
- Registration reveals whether an email is already registered (`409`). This comes with unique emails, and there is no rate limiting yet.
- A duplicate registration is detected only after the password is hashed, so it still costs one bcrypt round.
- There are no refresh tokens and no logout. Apart from deleting the account, a token cannot be revoked before it expires. Keep `JWT_TTL` short.
- There are no roles. Any logged-in user may create users and read any user, but may change or delete only their own account, and there is no administrator who can change or delete others.
- A password cannot be changed after registration.
- The user list has no total count. With offset pagination, a user created while a client is paging shifts later pages by one, so a client can see the same user twice. Large offsets also get slower, because MongoDB still walks the skipped entries. Cursor (keyset) pagination on `(created_at, _id)` would fix both.
- Login has no rate limiting or lockout, so password guessing is limited only by bcrypt's cost.
- The dummy hash for unknown-email logins is computed on the first such request after startup, so that one request takes about twice as long.
