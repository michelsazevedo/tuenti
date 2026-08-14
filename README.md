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


## License
Copyright © 2026
