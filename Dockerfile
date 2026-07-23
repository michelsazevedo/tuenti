# syntax=docker/dockerfile:1
# check=error=true

# Dev stage
FROM golang:1.26.1 AS dev

ENV APP_HOME=/go/src/github.com/michelsazevedo/tuenti/
WORKDIR $APP_HOME

COPY go.mod ./

RUN go mod download && go mod verify

COPY . .

# Builder stage
FROM dev AS builder

ENV APP_HOME=/go/src/github.com/michelsazevedo/tuenti
WORKDIR $APP_HOME

RUN CGO_ENABLED=0 GOOS=linux go build -o tuenti ./cmd

# Production stage
FROM alpine:latest AS production

ENV APP_HOME=/go/src/github.com/michelsazevedo/tuenti/

COPY --from=builder $APP_HOME .

EXPOSE 8080

CMD ["./tuenti"]
