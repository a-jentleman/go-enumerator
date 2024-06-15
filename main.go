// go-enumerator is a tool designed to be called by go:generate for generating enum-like
// code from constants.
//
// See [README] for more documentation
//
// [README]: https://pkg.go.dev/gitlab.com/panicrx/go-enumerator
package main

import (
	"gitlab.com/panicrx/go-enumerator/internal/cmd"
)

func main() {
	cmd.Execute()
}
