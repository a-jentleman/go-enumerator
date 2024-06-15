## Description

`go-enumerator` is a code generation tool designed for making constants behave more
like enums. The generated methods allow users to:

1. Convert numeric constants to string representations using `fmt.Print(x)`
2. Parse string representations using `fmt.Scan("Name", &x)`
3. Check if variables hold valid enum values using `x.Defined()`
4. Iterate through all defined enum values using `x.Next()`
5. Marshal and Unmarshal into text/json using `x.MarshalText()` and `x.UmarshalText()`

`go-enumerator` is designed to be invoked by `go generate`,
but it can be used as a command-line tool as well.

---

[![Go Reference](https://pkg.go.dev/badge/gitlab.com/panicrx/go-enumerator.svg)](https://pkg.go.dev/gitlab.com/panicrx/go-enumerator) <br />
Additional documentation available at [pkg.go.dev](https://pkg.go.dev/gitlab.com/panicrx/go-enumerator)

## Installation

Installation is easy, just install the package using the `go install` tool.

```shell
    go install gitlab.com/panicrx/go-enumerator
```

## Overview

Below is an example of the intended use for `go-enumerate`.
All command line arguments are optional `go generate`.
The tool will use the `$GOFILE`, `$GOPACKAGE`, and `$GOLINE` environment variables
to find the type declaration immediately following to `//go:generate` comment.

```go
//go:generate go-enumerator
type Kind int

const (
	Kind1
	Kind2
	Kind3 // DifferentString
)
```

In this case, we found the `Kind` type, which is a suitable type for generating an enum definition for.
The following methods are created in a new file with the default file name.

```go
// String implements fmt.Stringer
func (sut Kind) String() string { /* omitted for brevity */ }

// Scan implements fmt.Scanner
func (sut *Kind) Scan(ss fmt.ScanState, verb rune) error { /* omitted for brevity */ }

// Defined returns true if sut holds a defined value
func (sut Kind) Defined() bool { /* omitted for brevity */ }

// Next returns the next defined value after sut
func (sut Kind) Next() Kind { /* omitted for brevity */ }

// MarshalText implements encoding.TextMarshaler
func (sut Kind) MarshalText() ([]byte, error) { /* omitted for brevity */ }

// UnmarshalText implements encoding.TextUnmarshaler
func (sut *Kind) UnmarshalText([]byte) error { /* omitted for brevity */ }
```

`String()` and `Scan()` can be used in conjunction with the `fmt` package to parse
and encode values into human-friendly representations.

`Next()` can be used to loop through all defined values for an _enum_.

`Defined()` can be used to ensure that a given variable holds a defined value.

`MarshalText` and `UnmarshalText` can be used by themselves, but they are also
used by `encoding/json` and other text-based encoding packages.

### Remarks

- `go-enumerator` was inspired by [stringer](https://pkg.go.dev/golang.org/x/tools/cmd/stringer), which is a better `String()` generator. If all you need is a `String()` method for a numeric constant, consider using that tool instead.
- Examples for how to use the generated code can be found at [https://pkg.go.dev/gitlab.com/panicrx/go-enumerator/example](https://pkg.go.dev/gitlab.com/panicrx/go-enumerator/example)
- If you find this tool useful, give the repo a star! Feel free leave issues and/or suggest fixes or improvements as well 🙂
