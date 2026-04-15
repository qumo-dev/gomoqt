---
title: Migrate
weight: 11
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

Use `OnGoaway` on `moqt.Server` to handle notifications from upstream:

```go
    server := moqt.Server{
        OnGoaway: func(newSessionURI string) {
            slog.Info("Upstream requested migration", "uri", newSessionURI)
        },
        // ...
    }
```