package main
import (
	"fmt"
	"io/ioutil"
	"strings"
)
func main() {
	b, _ := ioutil.ReadFile("coverage.out")
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, "message_reader.go") && strings.HasSuffix(l, " 0") {
			fmt.Println(l)
		}
	}
}
