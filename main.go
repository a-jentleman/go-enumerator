// go-enumerator is a tool designed to be called by go:generate for generating enum-like
// code from constants.
//
// See [README] for more documentation
//
// [README]: https://pkg.go.dev/github.com/a-jentleman/go-enumerator
package main

import (
	"github.com/a-jentleman/go-enumerator/internal/cmd"
)

func main() {
	cmd.Execute()
}
