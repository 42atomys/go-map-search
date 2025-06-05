package engine

import (
	"sync"
)

// SearchResult represents a single search result with its relevance score
type SearchResult struct {
	ID    string  // Document identifier
	Text  string  // Original document text
	Score float32 // Relevance score (higher = more relevant)
}

// RuntimeSearch handles the core search functionality with minimal allocations
type RuntimeSearch struct {
	mu             sync.RWMutex
	cachedData     map[string]string   // Original data cache
	cachedWordMap  map[string][]string // Word -> document IDs mapping
	cachedTrigrams map[string][]string // Trigram -> document IDs mapping

	// Pre-allocated working memory - larger sizes to avoid reallocation
	indexBuffer    [4096]byte
	indexBufferLen int
}

// SearchEngine is the main interface for performing searches
type SearchEngine struct {
	rs *RuntimeSearch
}

// RuntimeSearch pool for QuickSearch to avoid allocation
var runtimeSearchPool = sync.Pool{
	New: func() interface{} {
		return NewRuntimeSearch()
	},
}

// Pre-computed lookup table for word boundaries - faster than switch/if chains
var wordBoundaryLUT = [256]bool{
	// Initialize with common word boundary characters
	' ': true, '\t': true, '\n': true, '\r': true,
	'.': true, ',': true, ';': true, ':': true,
	'!': true, '?': true, '-': true, '_': true,
	'/': true, '\\': true, '(': true, ')': true,
	'[': true, ']': true, '{': true, '}': true,
	'"': true, '\'': true,
}

// NewSearchEngine creates a new search engine instance
func NewSearchEngine() *SearchEngine {
	return &SearchEngine{
		rs: NewRuntimeSearch(),
	}
}

// Search performs a search with ONE allocation for the result slice
// This is the safest API - results are stable and won't be corrupted by subsequent searches
func (se *SearchEngine) Search(data map[string]string, query string, maxResults int) []SearchResult {
	if maxResults <= 0 || len(data) == 0 || len(query) == 0 {
		return nil
	}

	const cacheThreshold = 1000

	if len(data) <= cacheThreshold {
		return se.rs.performSearchOneAlloc(data, query, maxResults, false)
	}
	return se.rs.performSearchOneAlloc(data, query, maxResults, true)
}

// SearchInto performs a search with ZERO allocations using caller-provided buffer
// Returns slice view into the provided buffer. Caller owns the memory.
// This is the fastest API - no allocations, but results can be corrupted by subsequent searches on the same resultBuffer
func (se *SearchEngine) SearchInto(data map[string]string, query string, resultBuffer []SearchResult) []SearchResult {
	if len(resultBuffer) == 0 || len(data) == 0 || len(query) == 0 {
		return nil
	}

	const cacheThreshold = 1000
	maxResults := len(resultBuffer)

	if len(data) <= cacheThreshold {
		return se.rs.performSearchZeroAlloc(data, query, maxResults, false, resultBuffer)
	}
	return se.rs.performSearchZeroAlloc(data, query, maxResults, true, resultBuffer)
}

// QuickSearch performs a direct search without caching - ONE allocation for results
// This is the safest API - results are stable and won't be corrupted
func QuickSearch(data map[string]string, query string, maxResults int) []SearchResult {
	if maxResults <= 0 || len(data) == 0 || len(query) == 0 {
		return nil
	}

	// Get RuntimeSearch from pool to avoid allocation
	rs := runtimeSearchPool.Get().(*RuntimeSearch)
	defer runtimeSearchPool.Put(rs)

	return rs.performSearchOneAlloc(data, query, maxResults, false)
}

// QuickSearchInto performs a direct search with ZERO allocations using caller-provided buffer
// This is the fastest API - no allocations, but results can be corrupted by subsequent searches on the same resultBuffer
func QuickSearchInto(data map[string]string, query string, resultBuffer []SearchResult) []SearchResult {
	if len(resultBuffer) == 0 || len(data) == 0 || len(query) == 0 {
		return nil
	}

	// Get RuntimeSearch from pool to avoid allocation
	rs := runtimeSearchPool.Get().(*RuntimeSearch)
	defer runtimeSearchPool.Put(rs)

	maxResults := len(resultBuffer)
	return rs.performSearchZeroAlloc(data, query, maxResults, false, resultBuffer)
}

// compareScoreAndID returns comparison result for score+ID pairs to ensure
// deterministic ordering.
func compareScoreAndID(score1 float32, id1 string, score2 float32, id2 string) int {
	if score1 > score2 {
		return 1
	} else if score1 < score2 {
		return -1
	} else if id1 < id2 {
		return 1
	} else if id1 > id2 {
		return -1
	}
	return 0
}

// Fast byte-level operations

// containsAnyQueryBytes quickly checks if doc contains any query bytes
// using a byte lookup table for O(1) average time complexity per byte.
func containsAnyQueryBytes(doc, query []byte) bool {
	if len(query) == 0 {
		return false
	}

	// Create quick lookup table for query bytes
	var queryBytes [256]bool
	for _, b := range query {
		queryBytes[b] = true
	}

	// Scan document for any byte that matches a query byte
	for _, b := range doc {
		if queryBytes[b] {
			return true
		}
	}
	return false
}
