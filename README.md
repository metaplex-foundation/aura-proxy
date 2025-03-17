# Aura proxy

The Aura Proxy serves as an intermediary between users and processing providers.

This diagram illustrates the complete project architecture, with the red oval highlighting the functionality contained in this repository.

![](./doc/proxy.jpg)

## Overview

Aura Proxy functions as a request router that:

- Receives requests from users
- Redirects them to appropriate processing providers
- Manages request statistics and user data

The proxy communicates with the Aura API via gRPC to:

- Retrieve user information and subscription plans
- Store request statistics

All request statistics are initially cached locally on the proxy side before being periodically flushed and transmitted via gRPC to ClickHouse for permanent storage.

# How to launch

Aura Proxy requires the Aura API to be running before it can function properly.

Then `.env` file should be created. There is example config in `.env.example`.

Command to launch project without docker:

```
go run cmd/proxy/proxy.go --envFile .env
```

## Notes

If you don't put `nodeType` in `basicRouteNodes` config it will not be added as a target and requests will not be served.
