# Security

`mi` is local-first, but a city or coworking deployment still needs encrypted transport.

## Recommended deployment

For small communities, run the coordinator behind one of these:

- Tailscale or another WireGuard mesh.
- A reverse proxy with HTTPS.
- The built-in TLS mode with certificates trusted by every node and client.

Provider tokens and API keys protect access, but they do not encrypt traffic by themselves. Use HTTPS/WSS when traffic leaves one trusted machine.

## Built-in TLS

Generate development certificates:

```bash
make dev-certs
```

Start the coordinator on HTTPS:

```bash
make run-city-coordinator-tls
```

Start a node over WSS:

```bash
make run-city-node-tls
```

Call the API with the generated CA:

```bash
curl --cacert certs/ca.crt https://localhost:8443/network/status
```

## Real city networks

For a real deployment, generate certificates for the coordinator hostname that nodes will use:

```bash
COMMON_NAME=mi.example.local ALT_DNS=mi.example.local ALT_IP=100.64.0.10 make dev-certs
```

Then set:

```yaml
coordinator_url: "wss://mi.example.local:8443/ws/node"
tls:
  ca_file: "certs/ca.crt"
```

## Development escape hatch

The node agent supports:

```yaml
tls:
  insecure_skip_verify: true
```

Use this only for temporary local experiments. It disables certificate verification and should not be used for shared networks.
