package engine

import "sync"

// Context contains all pre-allocated buffers for zero-allocation search
type Context struct {
	// Text processing buffers - oversized to avoid reallocation
	queryNormalized [2048]byte // Large buffer for normalized query
	docNormalized   [8192]byte // Large buffer for normalized documents
	queryNormLen    int        // Actual length used in queryNormalized
	docNormLen      int        // Actual length used in docNormalized

	// Word boundary indices instead of string slices
	queryWordStarts [128]int // Start indices of words in queryNormalized
	queryWordEnds   [128]int // End indices of words in queryNormalized
	queryWordCount  int      // Number of words found

	docWordStarts [256]int // Start indices of words in docNormalized
	docWordEnds   [256]int // End indices of words in docNormalized
	docWordCount  int      // Number of words found

	// Candidate tracking without map allocation
	candidateIDs    [1024]string  // Pre-allocated candidate IDs
	candidateTexts  [1024]string  // Pre-allocated candidate texts
	candidateScores [1024]float32 // Pre-allocated candidate scores
	candidateCount  int           // Number of candidates

	// Candidate set tracking - use sorted slice instead of map
	candidateSet    [1024]string // Sorted list of candidate IDs
	candidateSetLen int          // Length of candidate set
}

// Zero-allocation context pool to reuse Context instances
var contextPool = sync.Pool{
	New: func() interface{} {
		return &Context{}
	},
}

// Reset clears the context for reuse without allocating
func (ctx *Context) reset() {
	ctx.queryNormLen = 0
	ctx.docNormLen = 0
	ctx.queryWordCount = 0
	ctx.docWordCount = 0
	ctx.candidateCount = 0
	ctx.candidateSetLen = 0
}
