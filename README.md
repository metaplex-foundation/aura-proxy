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

# Method-Based Routing Configuration in Aura Proxy

Aura Proxy supports a flexible method-based routing system that allows fine-grained control over how RPC methods are directed to different endpoints.

## Configuration Format

Aura Proxy configuration is provided through environment variables or `.env` files. For complex structures like the method-based routing configuration, JSON format is used.

## Configuration Example

The following example shows how to configure the `PROXY_SOLANA_CONFIG` environment variable:

```json
{
  "methodGroups": [
    {
      "name": "basic_methods",
      "methods": ["getAccountInfo", "getBlockHeight", "getBalance", "getSlot"]
    },
    {
      "name": "das_methods",
      "methods": [
        "getAsset", "getAssetBatch", "getAssetProof", "getAssetProofBatch", "getAssetsByOwner",
        "getAssetsByAuthority",  "getAssetsByCreator", "getAssetsByGroup", "getGrouping",
        "searchAssets", "getTokenAccounts", "getSignaturesForAsset", "getSignaturesForAssetV2", "getAuraHealth", "getAssets", "get_assets", "getAssetProofs", "get_asset_proofs",
        "getAssetSignatures", "get_asset_signatures", "getAssetSignaturesV2", "get_asset_signatures_v2", "getNftEditions", "get_nft_editions"
      ]
    },
    {
      "name": "transaction_methods",
      "methods": ["simulateTransaction", "sendTransaction"]
    }
  ],
  "providers": [
    {
      "name": "primary_provider",
      "endpoints": [
        {
          "url": "https://primary1.example.com/path",
          "weight": 20,
          "nodeType": {
            "name": "archive_node"
          },
          "methodGroups": ["basic_methods"],
          "handleOther": true
        },
        {
          "url": "https://primary2.example.com/path",
          "weight": 10,
          "nodeType": {
            "name": "archive_node"
          },
          "methodGroups": ["basic_methods", "transaction_methods"],
          "handleOther": true,
          "handleWebSocket": true
        },
        {
          "url": "https://primary3.example.com/path",
          "weight": 10,
          "nodeType": {
            "name": "archive_node"
          },
          "methodGroups": ["basic_methods"],
          "handleOther": true,
          "handleWebSocket": true
        }
      ]
    },
    {
      "name": "das_provider",
      "endpoints": [
        {
          "url": "https://das1.example.com",
          "weight": 10,
          "nodeType": {
            "name": "archive_node"
          },
          "methodGroups": ["das_methods"]
        },
        {
          "url": "https://das2.example.com",
          "weight": 10,
          "nodeType": {
            "name": "archive_node"
          },
          "methodGroups": ["das_methods"]
        },
        {
          "url": "https://das3.example.com",
          "weight": 10,
          "nodeType": {
            "name": "archive_node"
          },
          "methodGroups": ["das_methods"]
        },
        {
          "url": "https://das4.example.com",
          "weight": 10,
          "nodeType": {
            "name": "archive_node"
          },
          "methodGroups": ["das_methods"]
        }
      ]
    },
    {
      "name": "specialized_provider",
      "endpoints": [
        {
          "url": "https://specialized1.example.com",
          "weight": 10,
          "methods": ["getAssetProof"],
          "nodeType": {
            "name": "basic_node"
          }
        }
      ]
    }
  ]
}
```

## Configuration Options

### Method Groups

Method groups allow you to organize related methods together for easier reference:

```json
{
  "methodGroups": [
    {
      "name": "group_name",
      "methods": ["method1", "method2"]
    }
  ]
}
```

### Providers and Endpoints

Providers represent organizations offering RPC services, with each provider having one or more endpoints:

```json
{
  "providers": [
    {
      "name": "provider_name",
      "endpoints": [
        {
          "url": "https://endpoint-url.example.com"
          // Configuration for this endpoint
        }
      ]
    }
  ]
}
```

### Endpoint Configuration Options

Each endpoint can be configured with the following options:

- `url`: The RPC endpoint URL (required)
- `weight`: Relative weight for load balancing (default: 1.0)
- `nodeType`: Type of node and its capabilities
- `methods`: List of methods this endpoint explicitly handles
- `methodGroups`: References to predefined method groups
- `excludeMethods`: Methods to exclude from handling
- `handleOther`: Whether this endpoint handles methods not explicitly assigned elsewhere
- `handleWebSocket`: Whether this endpoint can handle WebSocket connections

## Important Notes on Method Handling

### Understanding `handleOther`

When an endpoint has `handleOther: true`, it will ONLY handle methods that are not explicitly assigned to any other endpoint. This means:

- It will NOT automatically handle methods that are specified on other endpoints, even if those endpoints are unavailable.
- You must explicitly list any method you want the endpoint to handle if that method is already specified on another endpoint.

### Understanding `handleWebSocket`

When an endpoint has `handleWebSocket: true`, it will be used for WebSocket connections:

- This flag identifies endpoints that can handle WebSocket protocol 
- The URL should be provided as standard HTTP/HTTPS URL (e.g., `https://example.com`)
- The proxy will automatically convert HTTP URLs to WebSocket URLs (ws/wss) when making WebSocket connections
- You should only set this on endpoints that support the WebSocket protocol
- You can have multiple WebSocket endpoints for load balancing and failover
- The router will distribute WebSocket connections based on endpoint weights (if specified)

### Example Scenarios

1. **Different methods on different endpoints**:
   - If `endpoint-A` handles `getAccountInfo` and `endpoint-B` handles `getTransaction`, each request will be routed to the appropriate endpoint.

2. **Fallback with `handleOther`**:
   - If `endpoint-A` explicitly lists `getAccountInfo` and `endpoint-B` has `handleOther: true`:
     - `getAccountInfo` will always be routed to `endpoint-A`
     - `endpoint-B` will never receive `getAccountInfo` requests, even if `endpoint-A` is down
     - Any method not explicitly listed on any endpoint will be routed to `endpoint-B`

3. **WebSocket handling**:
   - If `endpoint-A` has `handleWebSocket: true`:
     - WebSocket connections will be routed to `endpoint-A`
     - You can have multiple endpoints with `handleWebSocket: true` for load balancing
     - Regular HTTP requests will not be sent to WebSocket endpoints unless they also have methods listed

4. **Creating redundancy**:
   - To allow `endpoint-B` to handle `getAccountInfo` as a backup, you must add `getAccountInfo` explicitly to `endpoint-B`'s method list.
   - The router will then use the weight values to determine routing preferences.

### Method Exclusions

Use `excludeMethods` to prevent an endpoint from handling specific methods, even if they would otherwise be included via method groups:

```json
{
  "methodGroups": [
    {
      "name": "transaction_methods",
      "methods": ["getTransaction", "sendTransaction"]
    }
  ],
  "providers": [
    {
      "name": "provider_name",
      "endpoints": [
        {
          "url": "https://example.com",
          "methodGroups": ["transaction_methods"],
          "excludeMethods": ["sendTransaction"]
        }
      ]
    }
  ]
}
```

## Backward Compatibility

The configuration system maintains backward compatibility with legacy configurations:

- **DasAPINodes**: Automatically mapped to handle DAS methods
- **WSHostNodes**: Configured to handle WebSocket connections
- **BasicRouteNodes**: Used as default fallback routes - regular RPC

For production environments, we recommend using the new method-based configuration format for better control and clarity.

## Configuration Best Practices

### Weight Configuration

The `weight` property determines the relative probability of an endpoint being selected for a request:

- Higher weight values increase the likelihood of selection
- The default weight is 1.0 if not specified
- In the example above, the endpoint with weight 20 will receive approximately twice as many requests as endpoints with weight 10
- Use weights to:
  - Prioritize more reliable or faster endpoints
  - Direct more traffic to endpoints with higher capacity
  - Gradually shift traffic when adding new endpoints

### Performance Considerations

- The router makes routing decisions in memory, so even complex configurations have minimal performance impact
- For high-traffic deployments, consider using similar weights across endpoints in each category for predictable load distribution
