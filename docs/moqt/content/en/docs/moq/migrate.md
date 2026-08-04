---
title: Migrate
weight: 12
---

Session migration redirects connected clients to a new URI during graceful server shutdown.

## Server Side

Set `NextSessionURI` to specify the redirect destination, then call `Shutdown`:

```go
    server := moqt.Server{
        NextSessionURI: "https://backup.example.com/moq",
        // ...
    }

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    err := server.Shutdown(ctx)
```

## Client Side

Use `OnGoaway` to handle shutdown notifications:

```go
    dialer := moqt.Dialer{
        OnGoaway: func(newSessionURI string) {
            if newSessionURI != "" {
                slog.Info("Server requested migration", "uri", newSessionURI)
                // Reconnect to the new URI
            } else {
                slog.Info("Server is shutting down")
            }
        },
        // ...
    }
```

## Relay

A relay is a client with respect to its upstream, so it handles upstream migration
with the `moqt.Dialer` it uses to dial upstream — `moqt.Server` has no `OnGoaway` field:

```go
    // Upstream connection: react to the upstream server going away.
    dialer := moqt.Dialer{
        OnGoaway: func(newSessionURI string) {
            slog.Info("Upstream requested migration", "uri", newSessionURI)
            // Re-dial upstream and re-subscribe.
        },
        // ...
    }

    // Downstream connections: hand our own clients a new destination.
    server := moqt.Server{
        NextSessionURI: "https://relay-2.example.com/moq",
        // ...
    }
```