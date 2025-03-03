# syntax = docker/dockerfile:1.4
FROM quay.io/projectquay/golang:1.24 as builder

WORKDIR /app

RUN --mount=type=secret,id=github_pat \
    set -eux; \
    GIT_TOKEN="$(cat /run/secrets/github_pat)"; \
    git config --global url."https://${GIT_TOKEN}@github.com/".insteadOf "https://github.com/"; \
    go env -w GOPRIVATE=github.com

COPY . .

RUN make build

FROM alpine:3.21

WORKDIR /app

RUN apk add ca-certificates

COPY --from=builder /app/proxy /app

ENV PATH="/app:${PATH}"

ENTRYPOINT [ "./proxy" ]
