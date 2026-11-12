# Backend Golang Coding Test

This repository is a submission for a two-part backend coding test. Part 1 is a working Go service. Part 2 is a design proposal with no code, as the test asks. Each part has its own folder and does not depend on the other.

```
.
├── user-management-api/     Part 1: the Go service, its tests and Docker setup
└── lottery-search-system/   Part 2: the design document
```

## Part 1: User Management API

A REST API in Go for registering, logging in and managing users. It stores users in MongoDB through the official Go driver, hashes passwords with bcrypt, and protects endpoints with JWTs signed with HS256. A gRPC API offers `CreateUser` and `GetUser` over the same use cases, so validation and business rules exist only once.

The code follows a hexagonal (ports and adapters) layout: the domain and use cases know nothing about HTTP, gRPC or MongoDB, and each of those is an adapter around them. The service also logs every request with its method, path, status and duration, logs the number of users every 10 seconds from a background goroutine, and shuts down gracefully on `SIGINT` or `SIGTERM`.

Run it with Docker:

```bash
cd user-management-api
echo "JWT_SECRET=$(openssl rand -base64 48)" >> .env
docker compose up --build
```

The REST API is then on `http://localhost:8080` and gRPC on `localhost:9090`. The `.env` file stays on your machine and is git-ignored.

Run the tests (needs Go, Docker and make; `make check` also needs golangci-lint):

```bash
cd user-management-api
make test                            # unit tests
make db-up && make test-integration  # adds the MongoDB and end-to-end tests
make check                           # full gate: format, vet, lint, race detector, coverage thresholds
```

[user-management-api/README.md](user-management-api/README.md) has the rest:

- [Using the token](user-management-api/README.md#using-the-token): register, log in and call a protected endpoint
- [REST API](user-management-api/README.md#rest-api) and [gRPC](user-management-api/README.md#grpc): every endpoint and RPC with real requests, responses and error codes
- [Architecture](user-management-api/README.md#architecture): the layers and how a request flows through them
- [Testing and coverage](user-management-api/README.md#testing-and-coverage): how each layer is tested
- [Assumptions](user-management-api/README.md#assumptions), [Design decisions](user-management-api/README.md#design-decisions) and [Known limitations](user-management-api/README.md#known-limitations): readings of open points in the requirements, the reason behind each choice, and what is left out

## Part 2: Lottery Search System

A design for searching 10 million six-digit lottery tickets with wildcard patterns such as `****23` or `1****5`, where one search pattern must not give the same ticket to two users at the same time. The design document is [lottery-search-system/DESIGN.md](lottery-search-system/DESIGN.md).

The main ideas:

- **PostgreSQL is the source of truth** for the state of every ticket. Every change to a ticket's state is a conditional `UPDATE` inside one transaction.
- **Wildcard search runs on an in-memory digit bitmap index** in each API instance. There are only one million possible numbers but ten million tickets, so the index covers the numbers, not the tickets: one bitmap for each digit at each of the six positions, plus one bitmap of numbers that still have free tickets. A pattern ANDs the bitmaps of its fixed digits and skips its wildcards, so matching needs no database index.
- **A search hands out tickets as short holds.** Matching tickets are claimed with `SELECT ... FOR UPDATE SKIP LOCKED`, so two users searching the same pattern at once skip each other's rows instead of waiting or receiving the same ticket. A hold expires after 30 seconds unless the user reserves it.
- **The correctness path stays small:** only the stateless API and PostgreSQL (a primary, a synchronous standby and an asynchronous replica) decide who gets a ticket. Managed pieces around them absorb the spike before a draw: an edge waiting room and an API Gateway admit only the load the database can sustain, and Kafka carries reservation events and payment results but never allocates tickets. There is no Redis and no search engine.

The document reads "should not return the same ticket to multiple users at the same time" as "a search holds what it returns" (assumption A5, decision D1), and explains what changes if the authors meant only no double booking at reservation time. It also covers the database schema, the comparison of search approaches, the API, caching, scaling and performance, failure scenarios, monitoring, peak traffic, and records of the main architecture decisions.
