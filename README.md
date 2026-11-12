# User Management API

A user management API in Go, with a REST API and a gRPC API over the same use cases, MongoDB for storage, JWT (HS256) authentication, and a hexagonal architecture (ports and adapters). It also runs a background user-count worker, shuts down gracefully, and ships as a small Docker image.

The project was built one feature at a time. Each feature passed the full quality gate (`make check`) before the next one started.

## Quick start

```bash
echo "JWT_SECRET=$(openssl rand -base64 48)" >> .env
docker compose up --build
```

The REST API is then on `http://localhost:8080` and gRPC on `localhost:9090`. To try it, [register, log in and use the token](#using-the-token). For development without Docker, see [Running locally](#running-locally).

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
- HTTP, gRPC and worker adapters may not import the MongoDB driver or the repository adapter. They must go through use cases.

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
      └─ UserChecker port    → adapters/repository/mongodb (Exists: FindOne on _id, returning only _id)
  → adapters/http            put the user ID in the request context and call the handler

POST /api/v1/users
  → requireAuth, then the same Register use case as /api/v1/auth/register

GET /api/v1/users/{id}
  → adapters/http            requireAuth, then read {id} from the path
  → application/userservice  GetUser
      └─ UserRepository port → adapters/repository/mongodb (parse the ObjectID, then FindOne)

GET /api/v1/users?limit=&offset=
  → adapters/http            requireAuth, then parse limit and offset as integers (absent means 0)
  → application/userservice  ListUsers: a limit of 0 becomes 20, then check 1 ≤ limit ≤ 100 and offset ≥ 0
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

Every 10 seconds
  → adapters/worker          UserCountLogger: a ticker, stopped by the shutdown context
  → application/userservice  CountUsers
      └─ UserRepository port → adapters/repository/mongodb (CountDocuments)

gRPC user.v1.UserService/CreateUser and /GetUser
  → adapters/grpc            interceptor: authorization metadata → the same Authenticator as HTTP
  → application/userservice  the same Register and GetUser use cases as REST
  → adapters/grpc            map the user to the proto message, or the error to a gRPC status
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
├── .dockerignore              # keeps secrets, git data and build output out of the image build
├── Dockerfile                 # multi-stage build into a distroless, non-root image
├── .env.example               # environment variables for local runs
├── .golangci.yml              # linters and architecture rules
├── .mockery.yml               # mock generation for `make mocks`
├── Makefile                   # development and quality-gate commands
├── docker-compose.yml         # MongoDB and the API: `docker compose up --build`
├── go.mod
├── cmd/
│   └── api/                   # composition root: config, wiring, graceful shutdown
├── gen/
│   └── user/v1/               # code generated from proto/ by `make proto` (do not edit)
├── proto/
│   └── user/v1/user.proto     # gRPC contract
├── internal/
│   ├── domain/
│   │   └── user/              # User entity, validation rules, domain errors
│   ├── application/
│   │   └── userservice/       # use cases and the ports they need
│   ├── adapters/
│   │   ├── auth/              # bcrypt password hasher, JWT issuer
│   │   ├── grpc/              # gRPC server, auth interceptor, status mapping
│   │   ├── http/              # REST handlers, DTOs, error mapping, auth and logging middleware
│   │   ├── repository/
│   │   │   └── mongodb/       # UserRepository on MongoDB
│   │   └── worker/            # background user-count logger
│   └── infrastructure/
│       ├── config/            # environment variables
│       └── database/          # MongoDB connection
└── scripts/
    └── coverage-check.sh      # per-layer coverage gate
```

## Prerequisites

- Go 1.26 or newer
- Docker with Compose v2, for MongoDB and for running the whole stack
- [golangci-lint](https://golangci-lint.run/) v2
- make
- `protoc` only to regenerate the gRPC code (`brew install protobuf`). The generated code is committed, so building and testing do not need it.

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `HTTP_ADDR` | No | `:8080` | Address the HTTP server listens on |
| `GRPC_ADDR` | No | `:9090` | Address the gRPC server listens on |
| `MONGO_URI` | Yes | | MongoDB connection string |
| `MONGO_DATABASE` | No | `user_management` | Database name |
| `BCRYPT_COST` | No | `12` | bcrypt work factor, from 10 to 14 |
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

## Running with Docker

The whole stack, the API and MongoDB, runs with Docker Compose. Only a JWT secret is needed, which Compose reads from `.env`:

```bash
echo "JWT_SECRET=$(openssl rand -base64 48)" >> .env
docker compose up --build
```

`make up` does the same in the background and returns once both containers are healthy (about 13 seconds on a laptop). `make down` stops them.

| Service | Image | Listens on | Notes |
|---|---|---|---|
| `api` | Built from [Dockerfile](Dockerfile), 17.8 MB | `127.0.0.1:8080` (HTTP), `127.0.0.1:9090` (gRPC) | Starts only after `mongo` is healthy. Runs as a non-root user. |
| `mongo` | `mongo:8.0` | `127.0.0.1:27017` | Data in the named volume `mongo-data` |

Inside the Compose network, the API reaches MongoDB at `mongodb://mongo:27017`. Compose sets `MONGO_URI`, `HTTP_ADDR` and `GRPC_ADDR` itself, and takes `JWT_SECRET`, `JWT_ISSUER`, `JWT_TTL`, `BCRYPT_COST` and `MONGO_DATABASE` from `.env` or the shell. Without a secret, the `api` container exits at once with:

```json
{"level":"ERROR","msg":"api stopped","error":"config: JWT_SECRET is required and must be at least 32 bytes"}
```

Data survives `docker compose down` and `up`. `docker compose down -v` deletes the volume and every user in it.

### Health check

The image's `HEALTHCHECK` runs `/api healthcheck` every 10 seconds. That mode of the same binary calls `GET /healthz`, and exits 0 for `200` and 1 otherwise, because the distroless image has no shell, curl or wget. `docker compose ps` then shows `(healthy)`. In a real run, stopping MongoDB under the running API turned it `unhealthy` with `/healthz` returning `503`, and starting MongoDB again turned it back to `healthy`.

## REST API

All bodies are JSON. Errors share one shape:

```json
{"error":{"code":"<machine-readable code>","message":"<human-readable message>"}}
```

These apply to every path, in addition to each endpoint's own codes:

| Status | Code | When |
|---|---|---|
| 404 | `not_found` | No route matches the path |
| 405 | `method_not_allowed` | The path exists but not with this method. The `Allow` header lists the methods it accepts. |
| 499 | `request_canceled` | An `/api/v1` request whose use case stopped because the request itself was canceled, usually because the client disconnected or gave up. Logged at `INFO`, not as an error. Any other failure is still a `500`, even if the client has left. `/healthz` answers `503` instead. |

### GET /healthz

Auth: none

Reports whether the API can reach MongoDB. It pings with a 2-second limit and never returns the cause, which is logged at `ERROR` as `health check failed`. A probe that disconnects before the ping finishes is logged at `INFO` as `health check canceled`.

| Status | Body | When |
|---|---|---|
| 200 | `{"status":"ok"}` | MongoDB answered the ping |
| 503 | `{"status":"unavailable"}` | The ping failed or timed out |

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
{"error":{"code":"invalid_id","message":"id must be a 24-character lowercase hexadecimal string"}}
```

| Status | Code | When |
|---|---|---|
| 200 | | User found |
| 400 | `invalid_id` | The ID is not 24 lowercase hexadecimal characters |
| 401 | `unauthorized` | Token missing or rejected |
| 404 | `user_not_found` | No user has this ID |
| 500 | `internal_error` | Unexpected failure. Details are logged, never returned. |

### GET /api/v1/users

Auth: `Authorization: Bearer <access_token>`

Lists users, newest first. Users created in the same millisecond are ordered by ID, so pages never overlap or skip anyone because of a tie.

| Query parameter | Default | Allowed |
|---|---|---|
| `limit` | `20` | 1 to 100. Absent or `0` means the default. |
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
| 403 | `forbidden` | `{id}` is not exactly the caller's own ID, including a malformed ID |
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
| 403 | `forbidden` | `{id}` is not exactly the caller's own ID, including a malformed ID |
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

## gRPC

The gRPC server listens on `GRPC_ADDR` (default `:9090`) and serves [proto/user/v1/user.proto](proto/user/v1/user.proto):

```proto
service UserService {
  rpc CreateUser(CreateUserRequest) returns (CreateUserResponse);
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
}
```

Both RPCs call the same use cases as `POST /api/v1/users` and `GET /api/v1/users/{id}`, and follow the same rules. Both require a token, sent as `authorization` metadata in the same `Bearer <token>` form as the HTTP header and checked by the same `Authenticator`. A request with more than one `authorization` value is rejected.

Examples with [grpcurl](https://github.com/fullstorydev/grpcurl) from a real run. The server has no reflection, so grpcurl reads the proto file:

```bash
grpcurl -plaintext -import-path proto -proto user/v1/user.proto \
  -H "authorization: Bearer $TOKEN" \
  -d '{"name":"Bob Jones","email":"bob@example.com","password":"correct horse"}' \
  localhost:9090 user.v1.UserService/CreateUser
```

```json
{
  "user": {
    "id": "6af417b26a1c98ebb53907de",
    "name": "Bob Jones",
    "email": "bob@example.com",
    "createdAt": "2026-11-11T07:22:26.164Z"
  }
}
```

```bash
grpcurl -plaintext -import-path proto -proto user/v1/user.proto \
  -H "authorization: Bearer $TOKEN" -d '{"id":"6af400dac46deb7de7f3b26f"}' \
  localhost:9090 user.v1.UserService/GetUser
```

```text
ERROR:
  Code: NotFound
  Message: user not found
```

| Situation | Code | Message |
|---|---|---|
| Token missing, malformed or rejected | `Unauthenticated` | `a valid bearer token is required` |
| A field breaks a [user rule](#user-rules) | `InvalidArgument` | `request validation failed`, with a `google.rpc.BadRequest` detail listing each field |
| `id` is not 24 lowercase hexadecimal characters | `InvalidArgument` | `id must be a 24-character lowercase hexadecimal string` |
| No user has this ID | `NotFound` | `user not found` |
| Email already registered | `AlreadyExists` | `email is already registered` |
| The client canceled the call or its deadline passed, and the call stopped because of it | `Canceled` or `DeadlineExceeded` | Logged at `INFO` as `rpc canceled`, not as an error |
| Anything unexpected | `Internal` | `internal server error`, with the cause logged as `rpc failed` |

The `BadRequest` detail carries the same field messages as the REST `fields` array, for example `name: is required`. Go clients read it with `status.Convert(err).Details()`. grpcurl prints it as raw bytes, because it only loads `user.proto`.

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

When a request fails with `500`, a separate `ERROR` line named `request failed` records the real cause. The client only ever sees `internal_error`. A request that stopped because it was canceled (the client left, or a forced shutdown closed its connection) is logged at `INFO` as `request canceled`, so `ERROR` lines are left for real failures.

## Background worker

A goroutine started with the server logs the total number of users every 10 seconds. The first count comes 10 seconds after startup. Lines from a real run, where a second user registered between the two ticks:

```json
{"time":"2026-11-11T14:08:35.982431+07:00","level":"INFO","msg":"user count worker started","interval":"10s"}
{"time":"2026-11-11T14:08:45.989328+07:00","level":"INFO","msg":"user count","count":1}
{"time":"2026-11-11T14:08:55.987998+07:00","level":"INFO","msg":"user count","count":2}
{"time":"2026-11-11T14:08:56.114131+07:00","level":"INFO","msg":"user count worker stopped"}
```

If a count fails, the worker logs `count users failed` at `ERROR` with the cause and tries again at the next tick, so it never stops the API. A count interrupted by shutdown is not logged as an error.

## Graceful shutdown

`main` runs the HTTP server, the gRPC server and the worker in one `errgroup`, tied to a context that `SIGINT` (Ctrl+C) or `SIGTERM` cancels. On a signal:

1. The worker stops at its next select and logs `user count worker stopped`.
2. At the same time, under one 10-second deadline, the HTTP server and the gRPC server stop accepting new work and wait for in-flight requests (`http.Server.Shutdown`, `GracefulStop`). Work still running when the deadline passes is cut off: gRPC with `Stop`, HTTP with `http.Server.Close`. Both close the connections and cancel the handlers' contexts, but neither waits for those handlers to return, so one that is still unwinding may log a failed database call after MongoDB disconnects. The process then exits with status 1, because the shutdown was not clean.
3. The MongoDB client disconnects, and the process exits with status 0 after a clean shutdown.

In Docker, `docker compose stop` or `down` sends `SIGTERM`, and Compose waits up to 20 seconds (`stop_grace_period`) before killing the container: the 10-second budget above, plus up to 5 seconds for the MongoDB disconnect, with room to spare. In a real run the API stopped cleanly in about 1 second, with exit code 0.

A second signal during shutdown terminates the process immediately. Both listeners are opened before anything starts. If either cannot start, for example because a port is taken, nothing else starts, the error is logged as `api stopped`, and the process exits with status 1:

```json
{"time":"2026-11-11T14:09:47.782428+07:00","level":"ERROR","msg":"api stopped","error":"http server: listen tcp 127.0.0.1:8080: bind: address already in use"}
```

## Development commands

| Command | What it does |
|---|---|
| `make dev` | Run the API with live reload ([Air](https://github.com/air-verse/air)) |
| `make up` | Build and start the API and MongoDB in Docker, and wait until both are healthy |
| `make down` | Stop the API and MongoDB, keeping the data |
| `make db-up` | Start MongoDB in Docker and wait until it is healthy |
| `make db-down` | Stop the MongoDB container started by `make db-up`, keeping its data. `make down` stops the full stack. |
| `make mocks` | Regenerate test mocks from [.mockery.yml](.mockery.yml) |
| `make proto` | Regenerate `gen/` from `proto/`. Needs `protoc`. The Go plugins are installed at pinned versions into the git-ignored `bin/`. |
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
| `make vuln` | Run [govulncheck](https://go.dev/blog/govulncheck) v1.9.0. It reports only vulnerabilities the code can actually reach. |
| `make check` | Full gate: fmt-check → build → test → test-race → coverage-check → vet → lint |

## Testing and coverage

Tests use the standard `testing` package. Table-driven tests are the default style.

| Layer | How it is tested |
|---|---|
| Domain | Real values only. The domain has no dependencies. |
| Application | Ports replaced by generated mocks |
| HTTP | `httptest` through the real router, with the use case replaced by a generated mock. Bodies are compared exactly, so a leaked field fails the test. |
| gRPC | A real gRPC server and client over an in-memory `bufconn` connection, with mocked use cases and `Authenticator`. Assertions are on what a client actually receives: status codes, messages and `BadRequest` details. Plus end-to-end with the real JWT service and `Authenticator`. |
| Worker | `testing/synctest`, so the 10-second ticker runs on a fake clock: 35 simulated seconds take no real time. Covers counts per tick, nothing before the first tick, recovery after a failed count, a count cut short by shutdown, and a 1-minute fake-clock bound that fails fast if `Run` ignores cancellation. |
| Logging middleware | Status capture with small stub handlers, the full router for wiring, and `testing/synctest` for the duration: a handler that sleeps 150 ms on the fake clock must log exactly `150`, with no real waiting |
| Authentication | The `Authenticator` use case with mocked ports; header parsing with a mocked `Authenticator`; and end-to-end with the real JWT service and `Authenticator`: a valid token, an expired one, a foreign-secret one, and a valid token whose user was deleted |
| bcrypt hasher, JWT issuer, config | Real implementations. Issued tokens are parsed back and their claims checked. |
| MongoDB repository | Integration tests against a real MongoDB. Each test uses its own database and drops it afterwards. |
| End-to-end | `cmd/api` calls the real `run` function with test settings, so nothing is mocked: config, wiring, MongoDB, bcrypt, JWT, both servers, the worker and shutdown. See below. |

Mocks are generated by [mockery](https://vektra.github.io/mockery/) v3.9.1 with the `matryer` template. Each mock is a plain struct with a function field per method that records its calls, and it imports only the standard library. Tests never use testify or gomock. The mocks live next to their interface as `mocks_test.go`, so they compile only for tests. Run `make mocks` after changing a mocked interface, and commit the result with the interface.

To run the integration tests:

```bash
make db-up
make test-integration
```

### End-to-end tests

[cmd/api/main_integration_test.go](cmd/api/main_integration_test.go) starts the API the same way `main` does: it calls `run` with a cancellable context, a throwaway database and port `0`, and reads the real addresses from the `listening` log lines. It then walks one scenario over real HTTP and gRPC:

1. `/healthz`, registration, a duplicate email (`409`) and a wrong password (`401`)
2. Login, a protected route without a token (`401`), and creating a second user with a token
3. Listing newest first, renaming yourself, and being refused (`403`) when changing or deleting someone else or taking their email (`409`)
4. gRPC `GetUser` with the token from the REST login, a user created over gRPC and read over REST, a duplicate email (`AlreadyExists`) and a missing token (`Unauthenticated`)
5. Deleting your own account, after which the same token fails on both protocols and login fails
6. Cancelling the context: `run` returns `nil` after the worker, HTTP and gRPC servers log that they stopped
7. Checking the collected logs for any `ERROR` line, password, token or secret

A second table covers startup failures: invalid configuration, MongoDB unreachable, the HTTP port taken and the gRPC port taken. In each case `run` returns the error before any server reports `listening`, and when the gRPC port is taken the already-opened HTTP port is released. The whole run takes about 10 seconds.

These tests catch wiring mistakes that unit tests cannot see. With a missing unique index, a worker that never starts, a gRPC server that is never stopped, a leaked HTTP listener, or either server given an authenticator that skips the user check, they fail.

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
| `cmd/...` | Composition root. It only wires dependencies and is exercised by the end-to-end tests (`run`: 89.5% in the integration run). |
| `gen/...` | Generated protobuf code. |
| `internal/infrastructure/database` | Needs a real MongoDB, so it is covered by `make test-integration`. |
| `internal/adapters/repository/mongodb` | Needs a real MongoDB, so it is covered by `make test-integration`. |

Because of these exclusions, the gate's `OVERALL` figure can differ from the `total` printed by `go tool cover -func`, which counts every package.

## Assumptions

The requirements leave some points open. These are the readings this implementation takes:

- **"User registration" and "create a new user" are the same operation through two doors.** `POST /api/v1/auth/register` is public so anyone can sign up. `POST /api/v1/users` does the same for an authenticated caller. Both use one use case and the same rules.
- **There are no roles.** Any authenticated user may list and read every user. A user may update and delete only their own account (`403` otherwise), so one account cannot damage another.
- **A deleted user's tokens must stop working at once.** Every authenticated request therefore confirms that the token's user still exists.
- **"Update a user's name or email" is a partial update.** `PATCH` changes only the fields sent. The password cannot be changed, because that should require the current password, which the requirements do not cover.
- **Email addresses are case-insensitive.** They are trimmed and lowercased before they are stored or looked up, and uniqueness applies to that form.
- **"List all users" is paginated.** It returns at most 100 users per request (20 by default), newest first, so the response size stays bounded.
- **IDs are MongoDB ObjectIDs** in their canonical form: 24 lowercase hexadecimal characters. Any other spelling, including upper case, is rejected as `invalid_id`, so one account always has exactly one ID string. `PATCH` and `DELETE` check ownership first, so there any `{id}` that is not exactly the caller's ID gets `403`, malformed or not.
- **"Every 10 seconds" starts after startup.** The first count is logged 10 seconds after the API starts.
- **The gRPC server is secured.** "Optionally secure with token metadata" is read as a yes, and both RPCs take the same bearer token as REST.
- **"Mock MongoDB interactions where appropriate" means at the port.** Unit tests mock the `UserRepository` interface. The MongoDB adapter itself is tested against a real database, because a mocked driver would not show real behaviour such as the unique index.
- **TLS is out of scope for the service.** It is expected to end at a proxy or load balancer in front of the API.

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
- **Request bodies are decoded strictly.** Unknown fields are rejected, which blocks mass-assignment attempts such as `"role":"admin"`. The body must be one JSON value and at most 1 MiB. The limit is `http.MaxBytesHandler` around the whole router, outside the logging and route-error wrappers, so `net/http` sees the limit being hit and closes the connection instead of reading the rest of an oversized body.
- **Internal errors never reach the client.** Unexpected errors are logged with the method and path, and the client receives only `internal_error`. Passwords and hashes are never logged, and responses never contain a hash.
- **bcrypt cost defaults to 12.** Registration then takes about 230 ms on a laptop. `BCRYPT_COST` accepts 10 to 14: 10 is the common minimum recommendation, and 14 keeps one hash around 1 second on a laptop, well inside the 10-second HTTP write timeout.
- **Every MongoDB call has a timeout.** The client is created with a 5-second operation timeout, so a call without its own deadline cannot hang a request.
- **Login failures look the same.** An unknown email and a wrong password both return `401 invalid_credentials`. For an unknown email, the service still runs a bcrypt comparison against a dummy hash, so the response time does not reveal which emails are registered. In a local run, both cases took about 0.22 s.
- **Passwords over 72 bytes never match.** bcrypt's `CompareHashAndPassword` reads only the first 72 bytes, so a user with a 72-byte password could otherwise log in with that password plus any suffix. The hasher's `Compare` rejects longer passwords before calling bcrypt.
- **The JWT secret has no default.** `JWT_SECRET` must be set and at least 32 bytes long, and `.env.example` leaves it empty. The JWT adapter also refuses a shorter key itself, because golang-jwt signs with an empty key without complaint.
- **Only HS256 is issued, with minimal claims.** Tokens carry `sub`, `iss`, `iat` and `exp`, and nothing personal such as the email, because a JWT payload is readable by anyone who holds the token.
- **The composition root is testable.** `run(ctx, logger, getenv)` receives its context and environment from `main`, which only adds signal handling. The end-to-end tests therefore run the exact wiring that ships, not a copy of it, and stop it the same way a signal does.
- **The runtime image is distroless and non-root.** A multi-stage build compiles a static binary (`CGO_ENABLED=0`, `-trimpath`) and copies only that binary into `gcr.io/distroless/static-debian12:nonroot`. The result is 17.8 MB, with no shell, no package manager and no root user.
- **The binary checks its own health.** Distroless has no curl, so `/api healthcheck` performs the Docker health check. It calls `/healthz` on the configured HTTP address and relies on `net.Dial` treating `:8080` and `0.0.0.0:8080` as the local machine.
- **The health check runs every 10 seconds.** Docker 24 runs the first check only after one interval (`--start-interval` needs Docker 25), so a 30-second interval made `docker compose up --wait` take 32 seconds. With 10 seconds it takes 13. Each check also appears in the request log.
- **Compose does not refuse a missing secret; the API does.** `${JWT_SECRET:?...}` would stop Compose from parsing the file at all, which would also break `make db-up` and the integration tests for anyone without a `.env`. `${JWT_SECRET:-}` passes an empty value, and the API's own fail-fast check stops the container with a clear message.
- **Published ports are bound to `127.0.0.1`.** A local `docker compose up` does not expose the API or MongoDB to the rest of the network.
- **The Go toolchain is pinned to a patched release.** `go.mod` keeps `go 1.26.0` as the minimum, and `toolchain go1.26.11` makes the go command use 1.26.11 when the local Go is older (with the default `GOTOOLCHAIN=auto`). govulncheck found 19 standard-library vulnerabilities reachable from this code under go1.26.5, and none under go1.26.11. The Dockerfile pins `golang:1.26.11-alpine`, because the official Go images set `GOTOOLCHAIN=local` and would ignore the `toolchain` line. `golang.org/x/net` was raised to v0.62.0 for two advisories in its `html` package.
- **gRPC is a second front door, not a second implementation.** The gRPC adapter calls the same `Register` and `GetUser` use cases and the same `Authenticator` as HTTP, so validation, uniqueness and token rules exist once. It only maps requests to inputs, users to messages, and errors to status codes.
- **Both RPCs require a token,** matching their REST counterparts. Ambiguous metadata (two `authorization` values) is rejected instead of picking one.
- **Generated code is committed.** `gen/` is reproducible with `make proto`, but committing it means building, testing and running never need `protoc`. Plugin versions are pinned in the Makefile and installed into the project's `bin/`, not globally.
- **No server reflection.** Clients use the proto file, which is the contract anyway. Reflection would be a one-line addition if wanted.
- **The worker is a driving adapter.** Like the HTTP handlers, it calls a use case (`CountUsers`) through a one-method interface it declares, and depguard stops it from reaching the repository directly. The use case does not know a ticker exists.
- **One errgroup owns the long-running parts.** The server, the worker and the shutdown step share one cancellable context, so a signal or a server failure stops everything, and `main` returns the first real error. The listener is opened before anything starts, so `http server listening` is logged only once the port is actually bound.
- **Only the request's own cancellation is a cancellation.** A failure becomes `499` (REST) or `Canceled`/`DeadlineExceeded` (gRPC) only when the error wraps the request context's own error (`errors.Is(err, ctx.Err())`), which happens when the client leaves or a forced shutdown closes its connection. Anything else stays a `500` or `Internal` with an `ERROR` log. For example, MongoDB's own 5-second timeout while the client disconnects is still an error, so a real database failure is never hidden behind a client that happened to leave. The worker applies the same rule at shutdown.
- **The worker never takes the API down.** A failed count is logged and retried at the next tick. Errors caused by shutdown cancelling an in-flight count are expected, so they are not logged as errors.
- **Request logging wraps the whole router.** It sits outside routing and authentication, so it also records requests that never reach a handler: unknown paths, wrong methods, rejected tokens. Request lines are always `INFO`. The cause of a `500` is logged once at `ERROR` where it happens, not repeated by the request logger.
- **The status recorder keeps the writer's other features.** The wrapper records the first status, or `200` once a body is written, matching what `net/http` sends. It implements `Unwrap`, so `http.ResponseController` can still reach the real writer (for example to flush).
- **Authentication is a use case.** A token counts only while its user exists, which is a business rule, not just a JWT check. `userservice.Authenticator` verifies the token through the `TokenVerifier` port (the JWT adapter) and checks that the user still exists through the narrow `UserChecker` port. Its MongoDB adapter runs a `FindOne` on `_id` that returns only `_id`, so it stops at the first match and never loads the password hash. The HTTP middleware and the gRPC interceptor both call it, so the rule exists once. The JWT adapter still owns every signature and claim check.
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
- HTTP and gRPC are served without TLS. In production, TLS is expected to end at a load balancer or proxy in front of the API.
- A gRPC call whose deadline runs out during a MongoDB call can, in a narrow window, be logged as `rpc failed` and answered `Internal` instead of `rpc canceled`. The driver can report the deadline, or predict that it will be missed, a moment before the context itself reports it as expired. The client has already given up by then and sees `DeadlineExceeded` from its own deadline.
- govulncheck still lists one advisory at module level: `golang.org/x/crypto/openpgp` is unmaintained (GO-2026-5932). This code uses only `bcrypt` from that module and never calls `openpgp`, and no tagged release of `x/crypto` fixes it yet.
- A machine with `GOTOOLCHAIN=local` ignores the `toolchain` line, so a Go older than 1.26.11 should be upgraded by hand before running the API locally.
- gRPC calls are not written to the request log. Failed calls are logged as `rpc failed`, but there is no per-call line like the HTTP `http request` line.
- Registration reveals whether an email is already registered (`409`). This comes with unique emails, and there is no rate limiting yet.
- A duplicate registration is detected only after the password is hashed, so it still costs one bcrypt round.
- There are no refresh tokens and no logout. Apart from deleting the account, a token cannot be revoked before it expires. Keep `JWT_TTL` short.
- There are no roles. Any logged-in user may create users and read any user, but may change or delete only their own account, and there is no administrator who can change or delete others.
- A password cannot be changed after registration.
- The worker uses `CountDocuments`, which scans the `_id` index. That is exact and cheap at this scale. For a very large collection, `EstimatedDocumentCount` would be faster but approximate.
- The user list has no total count. With offset pagination, a user created while a client is paging shifts later pages by one, so a client can see the same user twice. Large offsets also get slower, because MongoDB still walks the skipped entries. Cursor (keyset) pagination on `(created_at, _id)` would fix both.
- Login has no rate limiting or lockout, so password guessing is limited only by bcrypt's cost.
- The dummy hash for unknown-email logins is computed on the first such request after startup, so that one request takes about twice as long. It uses the current `BCRYPT_COST`, so after the cost is changed, logins of users hashed at the old cost take a different time from unknown emails until those users are re-hashed (there is no re-hash on login).
