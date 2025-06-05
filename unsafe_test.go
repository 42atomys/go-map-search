package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnsafeBytesToString(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "Empty byte slice",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "Non-empty byte slice",
			input:    []byte{'h', 'e', 'l', 'l', 'o'},
			expected: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unsafeBytesToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestUnsafeStringToBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []byte
	}{
		{
			name:     "Empty string",
			input:    "",
			expected: []byte{},
		},
		{
			name:     "Non-empty string",
			input:    "hello",
			expected: []byte{'h', 'e', 'l', 'l', 'o'},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unsafeStringToBytes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMemEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        []byte
		b        []byte
		length   int
		expected bool
	}{
		{
			name:     "Equal slices",
			a:        []byte{'a', 'b', 'c', 'd'},
			b:        []byte{'a', 'b', 'c', 'd'},
			length:   4,
			expected: true,
		},
		{
			name:     "Different slices",
			a:        []byte{'a', 'b', 'c', 'd'},
			b:        []byte{'a', 'b', 'x', 'd'},
			length:   4,
			expected: false,
		},
		{
			name:     "Partial match",
			a:        []byte{'a', 'b', 'c', 'd'},
			b:        []byte{'a', 'b', 'x', 'd'},
			length:   2,
			expected: true,
		},
		{
			name:     "Empty slices",
			a:        []byte{},
			b:        []byte{},
			length:   0,
			expected: true,
		},
		{
			name:     "Different lengths",
			a:        []byte{'a', 'b', 'c'},
			b:        []byte{'a', 'b', 'c', 'd'},
			length:   3,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := memEqual(tt.a, tt.b, tt.length)
			assert.Equal(t, tt.expected, result)
		})
	}
}
