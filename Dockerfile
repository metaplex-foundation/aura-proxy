FROM golang:1.22-alpine3.19 as builder

WORKDIR /app

# used for build sqlite
RUN apk add --update gcc musl-dev git openssh-client

RUN git config --global url."git@github.com:".insteadOf "https://github.com/"
RUN go env -w GOPRIVATE=github.com

RUN mkdir ~/.ssh && echo "HOST *" > ~/.ssh/config
RUN echo "StrictHostKeyChecking no" >> ~/.ssh/config

COPY . .

RUN --mount=type=ssh CGO_ENABLED=0 GOOS=linux go build -a -v -installsuffix cgo ./cmd/proxy

FROM alpine:3.19
RUN apk add ca-certificates
#FIX of alpine can't find binary file
RUN apk add --no-cache libc6-compat
RUN apk add nmap
COPY --from=builder /app/proxy /usr/bin/

ENTRYPOINT [ "./proxy" ]