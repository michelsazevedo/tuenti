## Tuenti 
**Yet another invoice and accounting software**

Tuenti is a backend service that provides the core capabilities required to build invoicing, accounting, and financial management applications. 
It exposes a RESTful API for managing customers, invoices, payments, accounting records, and related financial workflows.


### Built With

- [Go](https://golang.org/)
- [Uber fx](https://github.com/uber-go/fx)
- [Echo](https://github.com/labstack/echo)
- [Postgres](https://www.postgresql.org/)
- [Redis](https://redis.io/)

Plus *some* of packages, a complete list of which is at [/master/go.mod](https://github.com/michelsazevedo/tuenti/blob/master/go.mod).

### Requirements
- Go 1.26+
- PostgreSQL 16+
- Kafka
- Docker (optional)

### Getting Started

#### Running with Make

Before running the application, copy `.env.example` to `.env` and fill in the values — the Makefile loads it automatically.

1. Run the local app with `make run` and tuenti will perform requests.

2. You can run the automated tests suite running a `make test` with no other parameters!

#### Running with Docker
[Docker](www.docker.com) is an open platform for developers and sysadmins to build, ship, and run distributed applications, whether on laptops, data center VMs, or the cloud.

If you haven't used Docker before, it would be good idea to read this article first: Install [Docker Engine](https://docs.docker.com/engine/installation/)

Before running the application, copy `.env.example` to `.env` and fill in the values.

1. Install [Docker](https://www.docker.com/what-docker) and then [Docker Compose](https://docs.docker.com/compose/):

2. Run `docker compose build --no-cache` to build the images for the project.

3. Finally, run the local app with `docker compose up web` and tuenti will perform requests.

4. Aaaaand, you can run the automated tests suite running a `docker compose run --rm test` with no other parameters!

### Scheduled Jobs

`cmd/jobs` runs the expired-trial suspension sweep once, moving every organization whose trial window has closed from `trialing` to `suspended`, then exits.

```ssh
go run ./cmd/jobs
```

On success, it logs a structured summary (`found` / `suspended` / `failed`) and exits `0` — including when zero trials had expired.

On failure, it logs why (boot failure, or a suspension that failed) and exits `1`.

### Database Seeds

`cmd/seed` populates reference data the application depends on, such as the list of Industries. It's idempotent, so running it any number of times never creates duplicates.

```ssh
go run cmd/seed/main.go
```

On success, it logs each seed's start/completion plus a final summary, and exits `0`.

On failure, it logs which seed failed and why, then exits `1`.

**Adding a new seed:** implement the `Seed` interface (`Run(ctx context.Context) error`) in a new file under `cmd/seed/seeds/`, then register an instance of it in the `registry` slice in `cmd/seed/main.go`. See `cmd/seed/seeds/industries.go` for a working example — its `industryNames` list is the single source of truth for that seed's data, so adding an industry is just adding a string there and re-running the command.


## License
Copyright © 2026
