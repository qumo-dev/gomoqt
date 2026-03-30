package main

import (
	"context"
	"log/slog"

	"github.com/okdaichi/gomoqt/moqt"
)

func main() {
	client := moqt.Dialer{
		Logger: slog.Default(),
	}

	_, err := client.Dial(context.Background(), "moqt://localhost:4469/nativequic", nil)
	if err != nil {
		slog.Error("failed to dial", "error", err)
		return
	}
}
