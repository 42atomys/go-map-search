package engine

import "unsafe"

// unsafeBytesToString converts []byte to string without allocation
// SAFE to use here because:
// 1. Query bytes are stable for the duration of the search
// 2. We only use this for temporary lookups in stable cached maps
func unsafeBytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return *(*string)(unsafe.Pointer(&b))
}

// unsafeStringToBytes converts string to []byte without allocation
// SAFE to use here because we only use this for temporary comparisons
func unsafeStringToBytes(s string) []byte {
	if s == "" {
		return []byte{}
	}
	return *(*[]byte)(unsafe.Pointer(&struct {
		string
		int
	}{s, len(s)}))
}

// memEqual memory comparison function that compares two byte slices
// for equality up to a specified length.
// It uses unsafe pointer arithmetic for potentially faster comparisons.
// This function is unsafe and should be used with caution.
func memEqual(a, b []byte, length int) bool {
	if length == 0 {
		return true
	}

	// Word-size comparison when possible (8 bytes at a time on 64-bit)
	const wordSize = unsafe.Sizeof(uintptr(0))

	// Handle word-aligned comparisons
	wordsToCompare := length / int(wordSize)
	for i := 0; i < wordsToCompare; i++ {
		aWord := *(*uintptr)(unsafe.Pointer(&a[i*int(wordSize)]))
		bWord := *(*uintptr)(unsafe.Pointer(&b[i*int(wordSize)]))
		if aWord != bWord {
			return false
		}
	}

	// Handle remaining bytes
	remaining := length % int(wordSize)
	offset := wordsToCompare * int(wordSize)
	for i := 0; i < remaining; i++ {
		if a[offset+i] != b[offset+i] {
			return false
		}
	}

	return true
}
