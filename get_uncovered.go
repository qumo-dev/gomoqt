package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func main() {
	cmd := exec.Command("go", "tool", "cover", "-func=coverage.out")
	out, _ := cmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "ReadMessageLength") {
			fmt.Println(line)
		}
	}
}
