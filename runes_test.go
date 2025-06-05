package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncodeRune(t *testing.T) {
	tests := []struct {
		name        string
		r           rune
		expected    []byte
		expectedLen int
	}{
		{
			name:        "ASCII lowercase",
			r:           'a',
			expected:    []byte{'a'},
			expectedLen: 1,
		},
		{
			name:        "ASCII uppercase",
			r:           'A',
			expected:    []byte{'a'},
			expectedLen: 1,
		},
		{
			name:        "2-byte rune",
			r:           'ñ',
			expected:    []byte{0xC3, 0xB1},
			expectedLen: 2,
		},
		{
			name:        "3-byte rune",
			r:           '漢',
			expected:    []byte{0xE6, 0xBC, 0xA2},
			expectedLen: 3,
		},
		{
			name:        "4-byte rune",
			r:           '😀',
			expected:    []byte{0xF0, 0x9F, 0x98, 0x80},
			expectedLen: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, 4)
			len := encodeRune(buf, tt.r)
			assert.Equal(t, tt.expectedLen, len)
			assert.Equal(t, tt.expected, buf[:len])
		})
	}
}

func TestDecodeRune(t *testing.T) {
	tests := []struct {
		name        string
		s           string
		expected    rune
		expectedLen int
	}{
		{
			name:        "ASCII character",
			s:           "a",
			expected:    'a',
			expectedLen: 1,
		},
		{
			name:        "2-byte rune",
			s:           "ñ",
			expected:    'ñ',
			expectedLen: 2,
		},
		{
			name:        "3-byte rune",
			s:           "漢",
			expected:    '漢',
			expectedLen: 3,
		},
		{
			name:        "4-byte rune",
			s:           "😀",
			expected:    '😀',
			expectedLen: 4,
		},
		{
			name:        "Invalid sequence",
			s:           "\xC3",
			expected:    0xFFFD,
			expectedLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, len := decodeRune(tt.s)
			assert.Equal(t, tt.expected, r)
			assert.Equal(t, tt.expectedLen, len)
		})
	}
}
