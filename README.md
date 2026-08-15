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
- Docker (optional)

### Getting Started

#### Running with Make

Before running the application, export the following variables:

```ssh
export POSTGRES_USER=
export POSTGRES_PASSWORD=
export POSTGRES_DB=
export POSTGRES_HOST=

export REDIS_HOST=
export REDIS_PASSWORD=

export RESEND_API_KEY=
export RESEND_FROM_EMAIL=

export PASSWORD_RESET_BASE_URL=
export EMAIL_CONFIRMATION_BASE_URL=
export INVITATION_BASE_URL=

export ENV_APP=development
export OTLP_ENDPOINT=
```

1. Run the local app with `make run` and tuenti will perform requests.

2. You can run the automated tests suite running a `make test` with no other parameters!

#### Running with Docker
[Docker](www.docker.com) is an open platform for developers and sysadmins to build, ship, and run distributed applications, whether on laptops, data center VMs, or the cloud.

If you haven't used Docker before, it would be good idea to read this article first: Install [Docker Engine](https://docs.docker.com/engine/installation/)

1. Install [Docker](https://www.docker.com/what-docker) and then [Docker Compose](https://docs.docker.com/compose/):

2. Run `docker compose build --no-cache` to build the images for the project.

3. Finally, run the local app with `docker compose up web` and tuenti will perform requests.

4. Aaaaand, you can run the automated tests suite running a `docker compose run --rm test` with no other parameters!

### Scheduled Jobs

`cmd/jobs` is a one-shot command: it runs the expired-trial suspension sweep once — moving every organization whose trial window has closed from `trialing` to `suspended` — and then exits. It starts no HTTP listener, and running it twice back to back is safe, since the second run finds nothing left to suspend.

Run it manually from the repository root:

```ssh
go run ./cmd/jobs
```

It must run from the repository root, because pending migrations are applied on boot from `./db/migrations` — the same way the API server does it.

It needs the **same environment variables as the main server** (the full list is under [Running with Make](#running-with-make)). That includes `RESEND_API_KEY`, `RESEND_FROM_EMAIL`, `PASSWORD_RESET_BASE_URL`, `EMAIL_CONFIRMATION_BASE_URL`, and `INVITATION_BASE_URL`, even though the job never sends email: configuration is validated as a whole rather than per binary, so it refuses to start when they are missing.

Exit codes:

| Code | Meaning |
| ---- | ------- |
| `0`  | The sweep completed, including when it found zero expired trials |
| `1`  | The job could not boot, or at least one organization failed to suspend |

Each run logs a structured summary (`found` / `suspended` / `failed`, plus the job outcome and duration) before exiting.

The daily cadence lives outside this repository: point an external scheduler — a Kubernetes `CronJob`, a cloud scheduler, or plain `cron` — at the command once a day and alert on a non-zero exit code. No deployment manifest is included here, as that is a deployment-environment concern.


## License
Copyright © 2026
