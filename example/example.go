package example

// Kind demonstrates integer style enums
//
//go:generate go-enumerator
type Kind int

const (
	Kind1 Kind = iota
	Kind2
	KindX // Kind3
)

// StrKind demonstrates string style enums
//
//go:generate go-enumerator
type StrKind string

const (
	Hello StrKind = "Hello"
	World StrKind = "World"
	Bang  StrKind = "Bang" // Override
)
