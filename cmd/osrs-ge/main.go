package main

import (
	"fmt"
	"os"

	"github.com/kierandotai/pp-osrs-ge/internal/osrsge"
)

func main() {
	if err := osrsge.Main(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
