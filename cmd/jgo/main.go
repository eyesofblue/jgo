package main

import (
	"fmt"
	"os"

	"github.com/eyesofblue/jgo/internal/command"
)

func main() {
	if err := command.Execute(os.Stdout, os.Stderr, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
