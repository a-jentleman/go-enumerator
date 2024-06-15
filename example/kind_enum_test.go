package example

import (
	"encoding"
	"fmt"
	"reflect"
	"testing"
)

func TestStrKind(t *testing.T) {
	kinds := [3]StrKind{
		Hello, World, Bang,
	}

	tests := []test[*StrKind, string]{
		{&kinds[0], "Hello", new(StrKind)},
		{&kinds[1], "World", new(StrKind)},
		{&kinds[2], "Override", new(StrKind)},
	}

	doTest(t, tests, func() *StrKind {
		ret := new(StrKind)
		*ret = "BADSTR"
		return ret
	})
}

func TestKind(t *testing.T) {
	kinds := [3]Kind{
		Kind1, Kind2, KindX,
	}

	tests := []test[*Kind, string]{
		{&kinds[0], "Kind1", new(Kind)},
		{&kinds[1], "Kind2", new(Kind)},
		{&kinds[2], "Kind3", new(Kind)},
	}

	doTest(t, tests, func() *Kind {
		ret := new(Kind)
		*ret = -1
		return ret
	})
}

type kindLike interface {
	Bytes() []byte
	fmt.Stringer
	fmt.Scanner
	encoding.TextMarshaler
	encoding.TextUnmarshaler
	Defined() bool
}

type test[sutT kindLike, repr string | int] struct {
	sut  sutT
	str  string
	zero sutT
}

func doTest[sutT kindLike, Repr string | int](t *testing.T, tests []test[sutT, Repr], invalidFunc func() sutT) {
	t.Helper()

	t.Run("String", func(t *testing.T) {
		for _, test := range tests {
			got := test.sut.String()
			if got != test.str {
				t.Errorf("String() = %v, want = %v", got, test.str)
			}
		}
	})

	t.Run("Bytes", func(t *testing.T) {
		for _, test := range tests {
			got := test.sut.Bytes()
			if string(got) != test.str {
				t.Errorf("Bytes() = %v, want = %v", got, test.str)
			}
		}
	})

	t.Run("Scan", func(t *testing.T) {
		for _, test := range tests {
			zero := test.zero
			fmt.Sscan(test.str, test.zero)
			if !reflect.DeepEqual(test.zero, test.sut) {
				t.Errorf("Scan() = %v, want = %v", test.zero, test.sut)
			}
			test.zero = zero
		}
	})

	t.Run("MarshalText", func(t *testing.T) {
		for _, test := range tests {
			got, err := test.sut.MarshalText()
			if err != nil {
				t.Error(err)
			}

			if string(got) != test.str {
				t.Errorf("MarshalText() = %v, want = %v", got, test.str)
			}
		}
	})

	t.Run("UnmarshalText", func(t *testing.T) {
		for _, test := range tests {
			zero := test.zero
			err := test.zero.UnmarshalText([]byte(test.str))
			if err != nil {
				t.Error(err)
			}

			if !reflect.DeepEqual(test.zero, test.sut) {
				t.Errorf("UnmarshalText() = %v, want = %v", test.zero, test.sut)
			}
			test.zero = zero
		}
	})

	t.Run("Defined", func(t *testing.T) {
		for _, test := range tests {
			got := test.sut.Defined()
			if !got {
				t.Errorf("Defined() = %v, want = %v", got, true)
			}
		}

		invalid := invalidFunc()
		if invalid.Defined() {
			t.Errorf("Defined() = %v, want = %v", true, false)
		}
	})
}
