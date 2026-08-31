package main

import (
	"fmt"
	"os"

	"github.com/ssj9685/groundspec/internal/core"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "groundspec: %s\n", err)
		os.Exit(3)
	}
	code, err := core.Execute(os.Args[1:], cwd, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "groundspec: %s\n", err)
		if !core.IsExpectedError(err) {
			code = 3
		}
	}
	os.Exit(code)
}
