package engine

// Fast rune encoding for common Unicode cases
func encodeRune(buf []byte, r rune) int {
	if r < 0x80 {
		if r >= 'A' && r <= 'Z' {
			buf[0] = byte(r + 32) // Convert to lowercase
		} else {
			buf[0] = byte(r)
		}
		return 1
	}

	if r < 0x800 {
		buf[0] = byte(0xC0 | r>>6)
		buf[1] = byte(0x80 | r&0x3F)
		return 2
	}

	if r < 0x10000 {
		buf[0] = byte(0xE0 | r>>12)
		buf[1] = byte(0x80 | (r>>6)&0x3F)
		buf[2] = byte(0x80 | r&0x3F)
		return 3
	}

	buf[0] = byte(0xF0 | r>>18)
	buf[1] = byte(0x80 | (r>>12)&0x3F)
	buf[2] = byte(0x80 | (r>>6)&0x3F)
	buf[3] = byte(0x80 | r&0x3F)
	return 4
}

// Fast rune decoding for common Unicode cases
func decodeRune(s string) (rune, int) {
	if len(s) == 0 {
		return 0, 0
	}

	b0 := s[0]
	if b0 < 0x80 {
		return rune(b0), 1
	}

	if len(s) < 2 {
		return 0xFFFD, 1 // Invalid
	}

	if b0 < 0xE0 { // 2-byte sequence
		return rune(b0&0x1F)<<6 | rune(s[1]&0x3F), 2
	}

	if len(s) < 3 {
		return 0xFFFD, 1
	}

	if b0 < 0xF0 { // 3-byte sequence
		return rune(b0&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}

	if len(s) < 4 {
		return 0xFFFD, 1
	}

	// 4-byte sequence
	return rune(b0&0x07)<<18 | rune(s[1]&0x3F)<<12 | rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F), 4
}
