package engine

import "math"

// NewRuntimeSearch creates a new runtime search instance
func NewRuntimeSearch() *RuntimeSearch {
	return &RuntimeSearch{}
}

// performSearchOneAlloc - allocates result slice (safe, no corruption)
func (rs *RuntimeSearch) performSearchOneAlloc(data map[string]string, query string, maxResults int, useCache bool) []SearchResult {
	// Get context from pool
	ctx := contextPool.Get().(*Context)
	defer func() {
		ctx.reset()
		contextPool.Put(ctx)
	}()

	// Normalize query with zero allocations
	rs.normalizeText(query, ctx.queryNormalized[:], &ctx.queryNormLen)
	rs.splitWords(ctx.queryNormalized[:ctx.queryNormLen], ctx.queryWordStarts[:], ctx.queryWordEnds[:], &ctx.queryWordCount)

	if useCache {
		rs.searchWithCache(data, ctx)
	} else {
		rs.searchDirect(data, ctx)
	}

	// Sort candidates by score (highest first), then by ID for determinism
	rs.sortCandidates(ctx)

	// Convert to results with ONE allocation for the result slice
	return rs.convertToResultsOneAlloc(ctx, maxResults)
}

// performSearchZeroAlloc - uses caller-provided buffer (zero allocation, caller owns memory)
func (rs *RuntimeSearch) performSearchZeroAlloc(data map[string]string, query string, maxResults int, useCache bool, resultBuffer []SearchResult) []SearchResult {
	// Get context from pool
	ctx := contextPool.Get().(*Context)
	defer func() {
		ctx.reset()
		contextPool.Put(ctx)
	}()

	// Normalize query with zero allocations
	rs.normalizeText(query, ctx.queryNormalized[:], &ctx.queryNormLen)
	rs.splitWords(ctx.queryNormalized[:ctx.queryNormLen], ctx.queryWordStarts[:], ctx.queryWordEnds[:], &ctx.queryWordCount)

	if useCache {
		rs.searchWithCache(data, ctx)
	} else {
		rs.searchDirect(data, ctx)
	}

	// Sort candidates by score (highest first), then by ID for determinism
	rs.sortCandidates(ctx)

	// Convert to results with ZERO allocations using caller's buffer
	return rs.convertToResultsZeroAlloc(ctx, maxResults, resultBuffer)
}

// normalizeText with SIMD-style optimizations
func (rs *RuntimeSearch) normalizeText(text string, buffer []byte, length *int) {
	*length = 0
	maxLen := len(buffer) - 4 // Reserve space for UTF-8

	// Process 8 bytes at a time when possible (word-size operations)
	i := 0
	textLen := len(text)

	// Fast path for ASCII-only text (most common case)
	for i < textLen && *length < maxLen {
		r := text[i]

		// Fast ASCII path - most common case
		if r < 128 {
			if r >= 'A' && r <= 'Z' {
				buffer[*length] = r + 32 // Convert to lowercase
			} else {
				buffer[*length] = r
			}
			*length++
			i++
		} else {
			// Handle Unicode - slower path
			rune, size := decodeRune(text[i:])
			if *length+4 <= maxLen { // Ensure space for UTF-8
				*length += encodeRune(buffer[*length:], rune)
			}
			i += size
		}
	}
}

// splitWords with lookup table and loops
func (rs *RuntimeSearch) splitWords(normalizedText []byte, starts []int, ends []int, count *int) {
	*count = 0
	start := 0
	maxWords := len(starts)
	if len(ends) < maxWords {
		maxWords = len(ends)
	}

	textLen := len(normalizedText)

	// loop with lookup table
	for i := 0; i < textLen && *count < maxWords; i++ {
		if wordBoundaryLUT[normalizedText[i]] { // Fast lookup instead of multiple comparisons
			if i > start {
				starts[*count] = start
				ends[*count] = i
				*count++
			}
			start = i + 1
		}
	}

	if start < textLen && *count < maxWords {
		starts[*count] = start
		ends[*count] = textLen
		*count++
	}
}

// searchDirect with early termination
func (rs *RuntimeSearch) searchDirect(data map[string]string, ctx *Context) {
	// Pre-calculate query characteristics for optimization
	hasLongWords := false
	for i := 0; i < ctx.queryWordCount; i++ {
		if ctx.queryWordEnds[i]-ctx.queryWordStarts[i] > 10 { // Long word
			hasLongWords = true
			break
		}
	}

	for id, text := range data {
		if ctx.candidateCount >= len(ctx.candidateIDs) {
			break
		}

		// Quick length check for optimization
		if hasLongWords && len(text) < ctx.queryNormLen/2 {
			continue // Skip obviously too-short documents
		}

		score := rs.scoreDocument(text, ctx)
		if score > 0 {
			ctx.candidateIDs[ctx.candidateCount] = id
			ctx.candidateTexts[ctx.candidateCount] = text
			ctx.candidateScores[ctx.candidateCount] = score
			ctx.candidateCount++
		}
	}
}

// searchWithCache with better cache utilization
func (rs *RuntimeSearch) searchWithCache(data map[string]string, ctx *Context) {
	// Check if we need to rebuild the cache
	rs.mu.RLock()
	needsRebuild := rs.cachedData == nil || len(rs.cachedData) != len(data)
	if !needsRebuild {
		// sample check - check fewer items but more efficiently
		checkCount := 0
		maxCheck := min(len(data)/10, 5) // Adaptive sample size
		for id, text := range data {
			if cachedText, exists := rs.cachedData[id]; !exists || cachedText != text {
				needsRebuild = true
				break
			}
			checkCount++
			if checkCount >= maxCheck {
				break
			}
		}
	}
	rs.mu.RUnlock()

	if needsRebuild {
		rs.buildIndex(data)
	}

	// Find candidates using cached indices
	rs.findCandidates(ctx)

	// Score candidates
	rs.scoreCandidates(ctx)
}

// findCandidates with better search strategy
func (rs *RuntimeSearch) findCandidates(ctx *Context) {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	ctx.candidateSetLen = 0

	// Find rarest word first for better filtering
	var rarest string
	minCount := int(^uint(0) >> 1) // Max int

	for i := 0; i < ctx.queryWordCount; i++ {
		start := ctx.queryWordStarts[i]
		end := ctx.queryWordEnds[i]
		queryWord := unsafeBytesToString(ctx.queryNormalized[start:end])

		if docIDs, exists := rs.cachedWordMap[queryWord]; exists && len(docIDs) < minCount {
			minCount = len(docIDs)
			rarest = queryWord
		}
	}

	// Start with rarest word if found
	if rarest != "" {
		if docIDs, exists := rs.cachedWordMap[rarest]; exists {
			rs.addToCandidateSet(docIDs, ctx)
		}
	}

	// Add other word matches
	for i := 0; i < ctx.queryWordCount; i++ {
		start := ctx.queryWordStarts[i]
		end := ctx.queryWordEnds[i]
		queryWord := unsafeBytesToString(ctx.queryNormalized[start:end])

		if queryWord == rarest {
			continue // Already processed
		}

		if docIDs, exists := rs.cachedWordMap[queryWord]; exists {
			rs.addToCandidateSet(docIDs, ctx)
		}

		// prefix matching with early termination
		prefixLen := end - start
		for word, docIDs := range rs.cachedWordMap {
			wordLen := len(word)

			// Quick length checks first
			if wordLen > prefixLen && wordLen-prefixLen <= 10 { // Reasonable prefix match
				if memEqual(unsafeStringToBytes(word), ctx.queryNormalized[start:end], prefixLen) {
					rs.addToCandidateSet(docIDs, ctx)
				}
			} else if prefixLen > wordLen && prefixLen-wordLen <= 10 {
				if memEqual(ctx.queryNormalized[start:start+wordLen], unsafeStringToBytes(word), wordLen) {
					rs.addToCandidateSet(docIDs, ctx)
				}
			}
		}
	}

	// Trigram fallback - only if no candidates and query is reasonable length
	if ctx.candidateSetLen == 0 && ctx.queryNormLen >= 3 && ctx.queryNormLen <= 100 {
		for i := 0; i <= ctx.queryNormLen-3; i += 2 { // Skip every other trigram for speed
			trigram := unsafeBytesToString(ctx.queryNormalized[i : i+3])
			if docIDs, exists := rs.cachedTrigrams[trigram]; exists {
				rs.addToCandidateSet(docIDs, ctx)
				if ctx.candidateSetLen > 100 { // Don't over-expand candidate set
					break
				}
			}
		}
	}
}

// addToCandidateSet with faster insertion
func (rs *RuntimeSearch) addToCandidateSet(docIDs []string, ctx *Context) {
	for _, docID := range docIDs {
		if ctx.candidateSetLen >= len(ctx.candidateSet) {
			break
		}

		// Binary search with manual inlining for speed
		left, right := 0, ctx.candidateSetLen
		for left < right {
			mid := (left + right) / 2
			if ctx.candidateSet[mid] < docID {
				left = mid + 1
			} else {
				right = mid
			}
		}

		// Check if already exists
		if left < ctx.candidateSetLen && ctx.candidateSet[left] == docID {
			continue
		}

		// Insert at position
		if ctx.candidateSetLen < len(ctx.candidateSet) {
			copy(ctx.candidateSet[left+1:ctx.candidateSetLen+1], ctx.candidateSet[left:ctx.candidateSetLen])
			ctx.candidateSet[left] = docID
			ctx.candidateSetLen++
		}
	}
}

// containsTrigram with word-aligned search
func (rs *RuntimeSearch) containsTrigram(text, trigram []byte) bool {
	if len(text) < 3 || len(trigram) != 3 {
		return false
	}

	// Create trigram word for faster comparison
	trigramWord := uint32(trigram[0])<<16 | uint32(trigram[1])<<8 | uint32(trigram[2])

	for i := 0; i <= len(text)-3; i++ {
		textWord := uint32(text[i])<<16 | uint32(text[i+1])<<8 | uint32(text[i+2])
		if textWord == trigramWord {
			return true
		}
	}
	return false
}

// containsSubsequence with better algorithm
func (rs *RuntimeSearch) containsSubsequence(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	if len(haystack) < len(needle) {
		return false
	}

	// Quick first/last byte check
	if len(needle) > 1 {
		firstByte := needle[0]
		lastByte := needle[len(needle)-1]

		// Find first occurrence of first byte
		firstPos := -1
		for i := 0; i <= len(haystack)-len(needle); i++ {
			if haystack[i] == firstByte {
				firstPos = i
				break
			}
		}

		if firstPos == -1 {
			return false
		}

		// Check if last byte exists in valid range
		found := false
		for i := firstPos + len(needle) - 1; i < len(haystack); i++ {
			if haystack[i] == lastByte {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}

	// Full substring search
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if memEqual(haystack[i:], needle, len(needle)) {
			return true
		}
	}
	return false
}

// sortCandidates with sorting
func (rs *RuntimeSearch) sortCandidates(ctx *Context) {
	n := ctx.candidateCount

	if n <= 1 {
		return
	}

	// Use sorting based on array size
	if n <= 10 {
		// Insertion sort for very small arrays
		rs.insertionSort(ctx, 0, n-1)
	} else if n <= 50 {
		// Shell sort for medium arrays
		rs.shellSort(ctx)
	} else {
		// Quicksort for larger arrays
		rs.quickSort(ctx, 0, n-1)
	}
}

// insertionSort for small arrays
func (rs *RuntimeSearch) insertionSort(ctx *Context, left, right int) {
	for i := left + 1; i <= right; i++ {
		score := ctx.candidateScores[i]
		id := ctx.candidateIDs[i]
		text := ctx.candidateTexts[i]

		j := i - 1
		for j >= left && (ctx.candidateScores[j] < score || (ctx.candidateScores[j] == score && ctx.candidateIDs[j] > id)) {
			ctx.candidateScores[j+1] = ctx.candidateScores[j]
			ctx.candidateIDs[j+1] = ctx.candidateIDs[j]
			ctx.candidateTexts[j+1] = ctx.candidateTexts[j]
			j--
		}

		ctx.candidateScores[j+1] = score
		ctx.candidateIDs[j+1] = id
		ctx.candidateTexts[j+1] = text
	}
}

// shellSort for medium-sized arrays
func (rs *RuntimeSearch) shellSort(ctx *Context) {
	n := ctx.candidateCount
	gaps := []int{5, 3, 1} // gap sequence

	for _, gap := range gaps {
		for i := gap; i < n; i++ {
			score := ctx.candidateScores[i]
			id := ctx.candidateIDs[i]
			text := ctx.candidateTexts[i]

			j := i
			for j >= gap && (ctx.candidateScores[j-gap] < score || (ctx.candidateScores[j-gap] == score && ctx.candidateIDs[j-gap] > id)) {
				ctx.candidateScores[j] = ctx.candidateScores[j-gap]
				ctx.candidateIDs[j] = ctx.candidateIDs[j-gap]
				ctx.candidateTexts[j] = ctx.candidateTexts[j-gap]
				j -= gap
			}

			ctx.candidateScores[j] = score
			ctx.candidateIDs[j] = id
			ctx.candidateTexts[j] = text
		}
	}
}

// quickSort with 3-way partitioning and insertion sort fallback
func (rs *RuntimeSearch) quickSort(ctx *Context, low, high int) {
	for low < high {
		// Use insertion sort for small subarrays
		if high-low < 10 {
			rs.insertionSort(ctx, low, high)
			break
		}

		// 3-way partitioning for better performance with duplicates
		lt, gt := rs.partition3Way(ctx, low, high)

		// Recursively sort smaller partition first (tail recursion optimization)
		if lt-low < high-gt {
			rs.quickSort(ctx, low, lt-1)
			low = gt + 1
		} else {
			rs.quickSort(ctx, gt+1, high)
			high = lt - 1
		}
	}
}

// partition3Way implements 3-way partitioning
func (rs *RuntimeSearch) partition3Way(ctx *Context, low, high int) (int, int) {
	pivot := ctx.candidateScores[low]
	pivotID := ctx.candidateIDs[low]

	lt := low      // ctx.candidateScores[low..lt-1] > pivot
	i := low + 1   // ctx.candidateScores[lt..i-1] = pivot
	gt := high + 1 // ctx.candidateScores[gt..high] < pivot

	for i < gt {
		cmp := compareScoreAndID(ctx.candidateScores[i], ctx.candidateIDs[i], pivot, pivotID)
		if cmp > 0 {
			rs.swapCandidates(ctx, lt, i)
			lt++
			i++
		} else if cmp < 0 {
			gt--
			rs.swapCandidates(ctx, i, gt)
		} else {
			i++
		}
	}

	return lt, gt - 1
}

// scoreCandidates with early termination
func (rs *RuntimeSearch) scoreCandidates(ctx *Context) {
	ctx.candidateCount = 0

	for i := 0; i < ctx.candidateSetLen && ctx.candidateCount < len(ctx.candidateIDs); i++ {
		docID := ctx.candidateSet[i]

		rs.mu.RLock()
		text, exists := rs.cachedData[docID]
		rs.mu.RUnlock()

		if exists {
			score := rs.scoreDocument(text, ctx)
			if score > 0 {
				ctx.candidateIDs[ctx.candidateCount] = docID
				ctx.candidateTexts[ctx.candidateCount] = text
				ctx.candidateScores[ctx.candidateCount] = score
				ctx.candidateCount++
			}
		}
	}
}

// scoreDocument with algorithmic improvements
func (rs *RuntimeSearch) scoreDocument(text string, ctx *Context) float32 {
	// Early exit for obviously bad matches
	if len(text) == 0 || ctx.queryWordCount == 0 {
		return 0
	}

	// Normalize document text
	rs.normalizeText(text, ctx.docNormalized[:], &ctx.docNormLen)

	// Quick scan for any query bytes before full word processing
	if !containsAnyQueryBytes(ctx.docNormalized[:ctx.docNormLen], ctx.queryNormalized[:ctx.queryNormLen]) {
		return 0 // Early exit if no common bytes
	}

	rs.splitWords(ctx.docNormalized[:ctx.docNormLen], ctx.docWordStarts[:], ctx.docWordEnds[:], &ctx.docWordCount)

	var totalScore float32
	exactMatches := 0

	// word matching with early termination
	for i := 0; i < ctx.queryWordCount; i++ {
		queryStart := ctx.queryWordStarts[i]
		queryEnd := ctx.queryWordEnds[i]
		queryLen := queryEnd - queryStart

		bestMatchForThisQuery := float32(0)

		// Quick first-byte filter before full comparison
		queryFirstByte := ctx.queryNormalized[queryStart]

		for j := 0; j < ctx.docWordCount; j++ {
			docStart := ctx.docWordStarts[j]
			docEnd := ctx.docWordEnds[j]
			docLen := docEnd - docStart

			// Quick first-byte check
			if ctx.docNormalized[docStart] != queryFirstByte && docLen != queryLen {
				continue
			}

			// Exact match check with comparison
			if queryLen == docLen {
				if memEqual(ctx.queryNormalized[queryStart:queryEnd], ctx.docNormalized[docStart:docEnd], queryLen) {
					bestMatchForThisQuery = 2.0
					exactMatches++
					break // Found exact match, no need to check prefixes
				}
			} else {
				// Prefix matching
				var prefixScore float32
				if docLen > queryLen {
					if memEqual(ctx.queryNormalized[queryStart:queryEnd], ctx.docNormalized[docStart:docStart+queryLen], queryLen) {
						prefixScore = 1.0
					}
				} else if queryLen > docLen {
					if memEqual(ctx.queryNormalized[queryStart:queryStart+docLen], ctx.docNormalized[docStart:docEnd], docLen) {
						prefixScore = 1.0
					}
				}
				if prefixScore > bestMatchForThisQuery {
					bestMatchForThisQuery = prefixScore
				}
			}
		}
		totalScore += bestMatchForThisQuery
	}

	// Early exit if score is already high enough
	if exactMatches == ctx.queryWordCount {
		return totalScore + float32(exactMatches-1)*0.5 // Skip other calculations
	}

	// Bonuses and fallbacks
	if exactMatches > 1 {
		totalScore += float32(exactMatches-1) * 0.5
	}

	if ctx.queryNormLen >= 3 && exactMatches == 0 && totalScore == 0 {
		substringScore := rs.scoreSubstring(ctx)
		totalScore += substringScore
	}

	if ctx.queryWordCount >= 2 && exactMatches < ctx.queryWordCount && totalScore < float32(ctx.queryWordCount) {
		reversedScore := rs.scoreReversedWords(ctx)
		totalScore += reversedScore
	}

	return totalScore
}

// scoreSubstring with faster trigram search
func (rs *RuntimeSearch) scoreSubstring(ctx *Context) float32 {
	if ctx.queryNormLen < 3 {
		return 0
	}

	matches := 0
	queryLen := ctx.queryNormLen

	// Use stride for faster search
	stride := max(1, queryLen/10) // Adaptive stride

	for i := 0; i <= queryLen-3; i += stride {
		trigram := ctx.queryNormalized[i : i+3]
		if rs.containsTrigram(ctx.docNormalized[:ctx.docNormLen], trigram) {
			matches++
		}
	}

	if matches == 0 {
		return 0
	}

	maxPossibleMatches := (queryLen-2)/stride + 1
	return float32(matches) / float32(maxPossibleMatches) * 0.3
}

// scoreReversedWords with better algorithm
func (rs *RuntimeSearch) scoreReversedWords(ctx *Context) float32 {
	if ctx.queryWordCount < 2 {
		return 0
	}

	matchCount := 0

	// Use more efficient matching strategy
	for i := 0; i < ctx.queryWordCount; i++ {
		queryStart := ctx.queryWordStarts[i]
		queryEnd := ctx.queryWordEnds[i]
		queryLen := queryEnd - queryStart

		// Skip very short query words
		if queryLen < 3 {
			continue
		}

		for j := 0; j < ctx.docWordCount; j++ {
			docStart := ctx.docWordStarts[j]
			docEnd := ctx.docWordEnds[j]
			docLen := docEnd - docStart

			// Quick length check
			if math.Abs(float64(docLen-queryLen)) > math.Min(float64(docLen), float64(queryLen))/2 {
				continue
			}

			if rs.containsSubsequence(ctx.docNormalized[docStart:docEnd], ctx.queryNormalized[queryStart:queryEnd]) ||
				rs.containsSubsequence(ctx.queryNormalized[queryStart:queryEnd], ctx.docNormalized[docStart:docEnd]) {
				matchCount++
				break
			}
		}
	}

	if matchCount >= 2 {
		return float32(matchCount) / float32(ctx.queryWordCount) * 0.8
	}
	return 0
}

// swapCandidates swaps two candidates
func (rs *RuntimeSearch) swapCandidates(ctx *Context, i, j int) {
	ctx.candidateScores[i], ctx.candidateScores[j] = ctx.candidateScores[j], ctx.candidateScores[i]
	ctx.candidateIDs[i], ctx.candidateIDs[j] = ctx.candidateIDs[j], ctx.candidateIDs[i]
	ctx.candidateTexts[i], ctx.candidateTexts[j] = ctx.candidateTexts[j], ctx.candidateTexts[i]
}

// convertToResultsOneAlloc allocates a new result slice (safe, no corruption)
func (rs *RuntimeSearch) convertToResultsOneAlloc(ctx *Context, maxResults int) []SearchResult {
	limit := min(ctx.candidateCount, maxResults)
	if limit == 0 {
		return nil
	}

	// Allocate new slice for results to prevent corruption
	results := make([]SearchResult, limit)
	for i := 0; i < limit; i++ {
		results[i].ID = ctx.candidateIDs[i]
		results[i].Text = ctx.candidateTexts[i]
		results[i].Score = ctx.candidateScores[i]
	}

	return results
}

// convertToResultsZeroAlloc uses caller-provided buffer (zero allocation)
func (rs *RuntimeSearch) convertToResultsZeroAlloc(ctx *Context, maxResults int, resultBuffer []SearchResult) []SearchResult {
	limit := min(ctx.candidateCount, maxResults)
	if limit > len(resultBuffer) {
		limit = len(resultBuffer)
	}

	if limit == 0 {
		return nil
	}

	// Copy into provided result buffer - NO ALLOCATION
	for i := 0; i < limit; i++ {
		resultBuffer[i].ID = ctx.candidateIDs[i]
		resultBuffer[i].Text = ctx.candidateTexts[i]
		resultBuffer[i].Score = ctx.candidateScores[i]
	}

	// Return slice view into provided buffer - NO ALLOCATION
	return resultBuffer[:limit]
}

// buildIndex builds search indices with optimizations
func (rs *RuntimeSearch) buildIndex(data map[string]string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// Clear and reuse existing maps
	if rs.cachedData == nil {
		rs.cachedData = make(map[string]string, len(data))
	} else {
		for k := range rs.cachedData {
			delete(rs.cachedData, k)
		}
	}

	if rs.cachedWordMap == nil {
		rs.cachedWordMap = make(map[string][]string, len(data)*3)
	} else {
		for k := range rs.cachedWordMap {
			delete(rs.cachedWordMap, k)
		}
	}

	if rs.cachedTrigrams == nil {
		rs.cachedTrigrams = make(map[string][]string, len(data)*5)
	} else {
		for k := range rs.cachedTrigrams {
			delete(rs.cachedTrigrams, k)
		}
	}

	// Build indices
	for docID, text := range data {
		rs.cachedData[docID] = text

		// Use instance buffers for normalization
		rs.normalizeText(text, rs.indexBuffer[:], &rs.indexBufferLen)

		// Create temporary slices for word indices
		var wordStarts [256]int
		var wordEnds [256]int
		var wordCount int

		rs.splitWords(rs.indexBuffer[:rs.indexBufferLen], wordStarts[:], wordEnds[:], &wordCount)

		// Index words
		for i := 0; i < wordCount; i++ {
			start := wordStarts[i]
			end := wordEnds[i]

			if start < end && end <= rs.indexBufferLen {
				word := string(rs.indexBuffer[start:end]) // Allocate string for cache key
				if existingIDs, exists := rs.cachedWordMap[word]; exists {
					rs.cachedWordMap[word] = append(existingIDs, docID)
				} else {
					rs.cachedWordMap[word] = []string{docID}
				}
			}
		}

		// Index trigrams with stride for efficiency
		if rs.indexBufferLen >= 3 {
			stride := max(1, rs.indexBufferLen/100) // Adaptive stride for large docs
			for i := 0; i <= rs.indexBufferLen-3; i += stride {
				trigram := string(rs.indexBuffer[i : i+3]) // Allocate string for cache key
				if existingIDs, exists := rs.cachedTrigrams[trigram]; exists {
					rs.cachedTrigrams[trigram] = append(existingIDs, docID)
				} else {
					rs.cachedTrigrams[trigram] = []string{docID}
				}
			}
		}
	}
}
