// Command projectctl provides repository-local development automation.
package main

import (
	"os"

	"github.com/ebe542/go-mediaarchive/internal/projecttool"
)

func main() {
	os.Exit(projecttool.Run(os.Args[1:], os.Stdout, os.Stderr))
}
