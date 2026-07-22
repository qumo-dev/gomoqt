package main

import (
	"fmt"
	"github.com/qumo-dev/gomoqt/moqt/internal/message"
)

func main() {
    var b []byte
    _, _, err := message.ReadBytes(b)
    fmt.Println(err)
}
