# Interop

Interop server and clients for testing MOQ Lite with WebTransport and QUIC.

> **TLS details:** the server uses a self-signed certificate checked in under `cmd/interop/server/`.
> the TypeScript client cannot trust this cert via the usual `mkcert` root bundle when talking
> over WebTransport, so we pin the certificate by its SHA‑256 hash. The wrapper `run_secure.ts`
> computes the hash automatically and passes `--insecure --cert-hash` to the client; you don't
> need to supply it yourself.
>
> The repository also includes a `Dockerfile` for containerized interop tests. The container
> image bundles Go, Deno and mkcert so the entire interop test can be run on any machine with
> Docker installed.

## Run

### Using Mage (from repository root)
```bash
# Preferred: run the full interop test inside Docker (self‑contained image)
# to exercise a particular client language:
mage interop:ts      # run TS client + server inside container
mage interop:go      # run Go client + server inside container
```

### Using Go directly
```bash
# from repository root

# Start the interop server
go run ./cmd/interop/server

# In another terminal, run the Go client
go run ./cmd/interop/client
```

Notes:
- Uses self-signed certificates in the repo; configure proper TLS for production.
- QUIC config enables datagrams and 0-RTT for low-latency exercises.