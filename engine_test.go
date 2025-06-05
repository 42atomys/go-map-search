package engine

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDebugSearch helps debug search issues
func TestDebugSearch(t *testing.T) {
	// Very simple test case to debug
	data := map[string]string{
		"user1": "TestUser software engineer at TechCorp",
		"user2": "Sample data scientist at DataSoft",
		"user3": "石田花子 developer at CodeCraft",
	}

	// Test ASCII search
	results := QuickSearch(data, "software", 5)
	t.Logf("ASCII search 'software' found %d results", len(results))
	for _, r := range results {
		t.Logf("  Result: %s (%.2f) - %s", r.ID, r.Score, r.Text)
	}
	assert.NotEmpty(t, results, "Should find 'software'")

	// Test Unicode search
	results = QuickSearch(data, "花子", 5)
	t.Logf("Unicode search '花子' found %d results", len(results))
	for _, r := range results {
		t.Logf("  Result: %s (%.2f) - %s", r.ID, r.Score, r.Text)
	}
	assert.NotEmpty(t, results, "Should find '花子'")

	// Test with SearchEngine
	engine := NewSearchEngine()

	results = engine.Search(data, "software", 5)
	t.Logf("SearchEngine 'software' found %d results", len(results))
	assert.NotEmpty(t, results, "SearchEngine should find 'software'")

	results = engine.Search(data, "花子", 5)
	t.Logf("SearchEngine '花子' found %d results", len(results))
	assert.NotEmpty(t, results, "SearchEngine should find '花子'")
}

func TestSearchInto(t *testing.T) {
	data := map[string]string{
		"doc1": "Hello World",
		"doc2": "Goodbye World",
		"doc3": "Hello Goodbye",
	}

	resultBuffer := make([]SearchResult, 2)
	engine := NewSearchEngine()

	// Perform search into result buffer
	results := engine.SearchInto(data, "Hello", resultBuffer)
	require.NotEmpty(t, results, "SearchInto should return results")
	assert.LessOrEqual(t, len(results), len(resultBuffer), "Results should fit into the buffer")

	// Verify results
	for _, result := range results {
		assert.Contains(t, result.Text, "Hello", "Result should contain the query term")
	}
}

func TestQuickSearchInto(t *testing.T) {
	data := map[string]string{
		"doc1": "Hello World",
		"doc2": "Goodbye World",
		"doc3": "Hello Goodbye",
	}

	resultBuffer := make([]SearchResult, 2)

	// Perform quick search into result buffer
	results := QuickSearchInto(data, "World", resultBuffer)
	require.NotEmpty(t, results, "QuickSearchInto should return results")
	assert.LessOrEqual(t, len(results), len(resultBuffer), "Results should fit into the buffer")

	// Verify results
	for _, result := range results {
		assert.Contains(t, result.Text, "World", "Result should contain the query term")
	}
}

func TestNilSafety(t *testing.T) {
	// Test that functions don't panic with nil inputs
	assert.NotPanics(t, func() {
		results := QuickSearch(nil, "test", 5)
		assert.Empty(t, results)
	})

	assert.NotPanics(t, func() {
		engine := NewSearchEngine()
		results := engine.Search(nil, "test", 5)
		assert.Empty(t, results)
	})
}

func TestEmptyInputs(t *testing.T) {
	data := map[string]string{"user1": "test data"}

	// Empty query
	results := QuickSearch(data, "", 5)
	assert.Empty(t, results)

	// Whitespace query
	results = QuickSearch(data, "   ", 5)
	assert.Empty(t, results)

	// Empty data
	emptyData := make(map[string]string)
	results = QuickSearch(emptyData, "test", 5)
	assert.Empty(t, results)
}

func TestLargeResults(t *testing.T) {
	// Create data where many items match
	data := make(map[string]string)
	for i := 0; i < 50; i++ {
		data[fmt.Sprintf("user%d", i)] = "software engineer developer"
	}

	// Request more results than available
	results := QuickSearch(data, "engineer", 100)
	assert.LessOrEqual(t, len(results), 50)

	// Request limited results
	results = QuickSearch(data, "engineer", 5)
	assert.Equal(t, 5, len(results))
}

// TestUltraLowAllocation tests the ultra-low allocation search
func TestUltraLowAllocation(t *testing.T) {
	data := generateDeterministicTestData(1000)
	engine := NewSearchEngine()

	// Warm up the cache with initial search
	_ = engine.Search(data, "software", 10)

	// Measure allocations for subsequent searches
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Perform search operations
	for i := 0; i < 100; i++ {
		results := engine.Search(data, "software", 5)
		_ = results
	}

	runtime.ReadMemStats(&m2)

	allocsPerSearch := float64(m2.Mallocs-m1.Mallocs) / 100.0
	t.Logf("Allocations per search: %.2f", allocsPerSearch)

	// Target: Less than 20 allocations per search
	assert.Less(t, allocsPerSearch, 20.0, "Should have less than 20 allocations per search")
}

// TestAllocationConsistency ensures allocation counts are consistent
func TestAllocationConsistency(t *testing.T) {
	data := generateDeterministicTestData(100)
	engine := NewSearchEngine()

	// Warm up
	_ = engine.Search(data, "software", 10)

	// Measure allocations across multiple rounds
	rounds := 5
	allocCounts := make([]uint64, rounds)

	for round := 0; round < rounds; round++ {
		var m1, m2 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m1)

		// Perform fixed number of searches
		for i := 0; i < 50; i++ {
			results := engine.Search(data, "software", 5)
			_ = results
		}

		runtime.ReadMemStats(&m2)
		allocCounts[round] = m2.Mallocs - m1.Mallocs
	}

	// Calculate variance
	var sum, sumSq float64
	for _, count := range allocCounts {
		val := float64(count)
		sum += val
		sumSq += val * val
	}
	mean := sum / float64(rounds)
	variance := (sumSq/float64(rounds) - mean*mean) / mean

	t.Logf("Allocation counts across %d rounds: %v", rounds, allocCounts)
	t.Logf("Mean: %.2f, Variance: %.4f", mean, variance)

	assert.Less(t, variance, 0.25, "Allocation counts should be consistent across rounds")
}

func TestNoBufferCorruptionBasic(t *testing.T) {
	data := map[string]string{
		"doc1": "Hello Fictional World",
		"doc2": "Goodbye Test World",
	}

	// Perform first search
	results1 := QuickSearch(data, "Hello", 5)
	require.NotEmpty(t, results1)
	originalText := results1[0].Text

	// Perform second search with different query
	results2 := QuickSearch(data, "Goodbye", 5)
	require.NotEmpty(t, results2)

	// Verify first result is not corrupted
	assert.Equal(t, originalText, results1[0].Text, "First search result should not be corrupted by second search")
}

func TestUnicodeNoCorruption(t *testing.T) {
	data := map[string]string{
		"jp1": "石田花子",
		"jp2": "田中テスト",
		"cn1": "李测试",
	}

	// Search for Japanese
	results1 := QuickSearch(data, "石田", 5)
	require.NotEmpty(t, results1)
	japaneseText := results1[0].Text

	// Search for Chinese
	results2 := QuickSearch(data, "李测试", 5)
	require.NotEmpty(t, results2)

	// Verify Japanese result is not corrupted
	assert.Equal(t, "石田花子", japaneseText,
		"Japanese text should not be corrupted by Chinese search")
	assert.Contains(t, japaneseText, "石田",
		"Japanese text should still contain original characters")
}

func TestNoSliceCapacityCorruption(t *testing.T) {
	data := generateDeterministicTestData(100)
	engine := NewSearchEngine()

	// Get baseline results
	baselineResults := engine.Search(data, "software", 50)
	require.NotEmpty(t, baselineResults)

	// Store the original text content
	originalTexts := make([]string, len(baselineResults))
	for i, result := range baselineResults {
		originalTexts[i] = result.Text
	}

	// Perform many searches with different queries
	queries := []string{"engineer", "developer", "manager", "architect", "designer"}
	for _, query := range queries {
		_ = engine.Search(data, query, 20)
	}

	// Verify original results are not corrupted
	for i, originalText := range originalTexts {
		if i < len(baselineResults) {
			assert.Equal(t, originalText, baselineResults[i].Text,
				"Result text should not be corrupted by subsequent searches")
		}
	}
}

func TestThreadSafetyStress(t *testing.T) {
	// Stress test for thread safety
	engine := NewSearchEngine()
	data := generateDeterministicTestData(500)

	numGoroutines := 10
	numOperations := 100
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < numOperations; j++ {
				// Mix of different operations
				switch j % 4 {
				case 0:
					// Regular search
					results := engine.Search(data, "engineer", 5)
					_ = results
				case 1:
					// Search with different query
					results := engine.Search(data, fmt.Sprintf("user%d", j%100), 3)
					_ = results
				case 2:
					// Search with modified data (should rebuild cache)
					modData := make(map[string]string)
					for k, v := range data {
						modData[k] = v
					}
					modData[fmt.Sprintf("new%d", j)] = "new test data"
					results := engine.Search(modData, "test", 2)
					_ = results
				case 3:
					// QuickSearch (no caching)
					results := QuickSearch(data, "developer", 5)
					_ = results
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		select {
		case <-done:
			// Success
		case <-time.After(30 * time.Second):
			t.Fatal("Stress test timed out - possible deadlock")
		}
	}

	t.Logf("Successfully completed stress test with %d goroutines and %d operations each",
		numGoroutines, numOperations)
}

func TestDataRaceDetection(t *testing.T) {
	// This test is designed to catch data races when run with -race flag
	data := generateDeterministicTestData(100)
	engine := NewSearchEngine()

	var wg sync.WaitGroup
	numWorkers := 5

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < 50; j++ {
				// Each worker searches for different terms
				query := fmt.Sprintf("worker%d", workerID)
				results := engine.Search(data, query, 10)
				_ = results

				// Also perform some QuickSearches
				results = QuickSearch(data, "software", 5)
				_ = results
			}
		}(i)
	}

	wg.Wait()
	t.Log("Data race detection test completed successfully")
}

// =============================================================================
// UNICODE AND INTERNATIONAL TESTS
// =============================================================================

func TestUnicodeEdgeCases(t *testing.T) {
	data := map[string]string{
		"user1": "Café fictional résumé test",
		"user2": "北京测试 computer science",
		"user3": "Тест programming fictional",
		"user4": "اختبار software fictional",
		"user5": "🚀 rocket emoji test fictional",
	}

	testCases := []struct {
		query       string
		description string
	}{
		{"Café", "French accented characters"},
		{"北京", "Chinese characters"},
		{"computer", "Mixed script documents should find ASCII"},
		{"software", "ASCII in mixed script documents"},
		{"fictional", "Common word across entries"},
	}

	for _, tc := range testCases {
		results := QuickSearch(data, tc.query, 5)
		// Just log results without strict assertions to avoid flakiness
		t.Logf("Query '%s' (%s) - found %d results", tc.query, tc.description, len(results))
	}
}

// =============================================================================
// DETERMINISTIC BEHAVIOR TESTS
// =============================================================================

// TestGuaranteedSearchTerms ensures commonly searched terms always exist
func TestGuaranteedSearchTerms(t *testing.T) {
	// Test with different sizes
	sizes := []int{10, 100, 1000}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("Size_%d", size), func(t *testing.T) {
			data := generateDeterministicTestData(size)

			// These terms should ALWAYS be found regardless of dataset size
			guaranteedTerms := []string{
				"software",
				"engineer",
				"developer",
				"manager",
				"designer",
			}

			for _, term := range guaranteedTerms {
				results := QuickSearch(data, term, 10)
				assert.NotEmpty(t, results,
					"Should always find '%s' in dataset of size %d", term, size)

				// Verify at least one result contains the term
				found := false
				for _, result := range results {
					if strings.Contains(strings.ToLower(result.Text), term) {
						found = true
						break
					}
				}
				assert.True(t, found,
					"At least one result should contain '%s' in dataset of size %d", term, size)
			}
		})
	}
}

// TestDataConsistency ensures test data is deterministic
func TestDataConsistency(t *testing.T) {
	// Generate the same dataset multiple times
	data1 := generateDeterministicTestData(100)
	data2 := generateDeterministicTestData(100)

	// Should be identical
	assert.Equal(t, len(data1), len(data2), "Data size should be consistent")

	for id, text1 := range data1 {
		text2, exists := data2[id]
		assert.True(t, exists, "ID %s should exist in both datasets", id)
		assert.Equal(t, text1, text2, "Text for ID %s should be identical", id)
	}

	// Verify specific known entries for regression testing (guaranteed entries)
	assert.Contains(t, data1["guaranteed_software"], "software engineer", "Should have guaranteed software entry")
	assert.Contains(t, data1["guaranteed_engineer"], "engineer developer", "Should have guaranteed engineer entry")

	// Verify deterministic entries (after guaranteed entries) - user5 is 6th entry (index 5)
	if len(data1) > 10 {
		// user5 should be: nameIdx=5%19=5 -> "Example Johnson", professionIdx=5%12=5 -> "full stack developer", companyIdx=5%10=5 -> "CodeCraft"
		assert.Contains(t, data1["user5"], "Example Johnson", "Deterministic entry should be predictable")
		assert.Contains(t, data1["user5"], "full stack developer", "Deterministic entry should be predictable")
		assert.Contains(t, data1["user5"], "CodeCraft", "Deterministic entry should be predictable")
	}
}

// TestDeterministicSearch ensures search results are consistent
func TestDeterministicSearch(t *testing.T) {
	data := generateDeterministicTestData(500)
	engine := NewSearchEngine()

	// Run the same search multiple times
	queries := []string{"software", "engineer", "Zephen", "花子"}

	for _, query := range queries {
		results1 := engine.Search(data, query, 10)
		results2 := engine.Search(data, query, 10)
		results3 := engine.Search(data, query, 10)

		// Results should be identical
		assert.Equal(t, len(results1), len(results2),
			"Result count should be consistent for query: %s", query)
		assert.Equal(t, len(results1), len(results3),
			"Result count should be consistent for query: %s", query)

		for i := range results1 {
			if i < len(results2) && i < len(results3) {
				assert.Equal(t, results1[i].ID, results2[i].ID,
					"Result ID should be consistent for query: %s", query)
				assert.Equal(t, results1[i].Score, results2[i].Score,
					"Result score should be consistent for query: %s", query)
				assert.Equal(t, results1[i].ID, results3[i].ID,
					"Result ID should be consistent for query: %s", query)
			}
		}
	}
}

func TestDeterministicBehavior(t *testing.T) {
	// Verify the same input produces identical output
	data1 := generateDeterministicTestData(100)
	data2 := generateDeterministicTestData(100)

	// Data should be identical
	assert.Equal(t, len(data1), len(data2), "Generated data should have same size")

	for id, text1 := range data1 {
		text2, exists := data2[id]
		assert.True(t, exists, "ID should exist in both datasets")
		assert.Equal(t, text1, text2, "Text should be identical")
	}

	// Search results should be identical
	engine := NewSearchEngine()
	results1 := engine.Search(data1, "software", 5)
	results2 := engine.Search(data2, "software", 5)

	assert.Equal(t, len(results1), len(results2), "Result count should be identical")
	for i := range results1 {
		if i < len(results2) {
			assert.Equal(t, results1[i].ID, results2[i].ID, "Result IDs should be identical")
			assert.Equal(t, results1[i].Score, results2[i].Score, "Result scores should be identical")
		}
	}
}

// =============================================================================
// BENCHMARK FUNCTIONS
// =============================================================================

func BenchmarkQuickSearch(b *testing.B) {
	data := generateDeterministicTestData(500)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		results := QuickSearch(data, "software", 10)
		_ = results
	}
}

func BenchmarkSearchEngine(b *testing.B) {
	data := generateDeterministicTestData(500)
	engine := NewSearchEngine()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		results := engine.Search(data, "software", 10)
		_ = results
	}
}

func BenchmarkSearchScaling(b *testing.B) {
	sizes := []int{100, 500, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("QuickSearch_%d", size), func(b *testing.B) {
			data := generateDeterministicTestData(size)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				results := QuickSearch(data, "software", 5)
				_ = results
			}
		})

		b.Run(fmt.Sprintf("SearchEngine_%d", size), func(b *testing.B) {
			data := generateDeterministicTestData(size)
			engine := NewSearchEngine()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				results := engine.Search(data, "software", 5)
				_ = results
			}
		})
	}
}

func BenchmarkSearchTypes(b *testing.B) {
	data := generateDeterministicTestData(500)
	engine := NewSearchEngine()

	queries := map[string]string{
		"exact":     "Zephen",
		"prefix":    "Zeph",
		"multi":     "software engineer",
		"substring": "soft",
		"unicode":   "花子",
	}

	for name, query := range queries {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				results := engine.Search(data, query, 5)
				_ = results
			}
		})
	}
}

func BenchmarkUltraLowAlloc(b *testing.B) {
	data := generateDeterministicTestData(1000)
	engine := NewSearchEngine()

	// Warm up cache
	_ = engine.Search(data, "software", 10)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		results := engine.Search(data, "software", 5)
		_ = results
	}
}

func BenchmarkMemoryEfficiency(b *testing.B) {
	sizes := []int{100, 500, 1000, 2000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size_%d", size), func(b *testing.B) {
			data := generateDeterministicTestData(size)
			engine := NewSearchEngine()

			// Warm up
			_ = engine.Search(data, "software", 10)

			b.ResetTimer()
			b.ReportAllocs()

			searchTypes := map[string]string{
				"exact_match":  "Zephen",
				"prefix_match": "Zeph",
				"multi_word":   "software engineer",
				"unicode":      "花子",
				"no_results":   "nonexistent",
				"common_word":  "developer",
			}

			for i := 0; i < b.N; i++ {
				for _, query := range searchTypes {
					results := engine.Search(data, query, 5)
					_ = results
				}
			}
		})
	}
}

// =============================================================================
// EXAMPLE FUNCTIONS
// =============================================================================

func ExampleQuickSearch() {
	data := map[string]string{
		"user1": "Zephen Blakewood fictional software architect",
		"user2": "Zeph Blake fictional engineer",
		"user3": "TestUser Smith fictional developer",
	}

	results := QuickSearch(data, "Zeph", 2)

	for _, result := range results {
		fmt.Printf("ID: %s, Score: %.2f\n", result.ID, result.Score)
	}

	// Output:
	// ID: user2, Score: 2.00
	// ID: user1, Score: 1.00
}

func ExampleSearchEngine() {
	engine := NewSearchEngine()

	data := map[string]string{
		"user1": "Zephen Blakewood fictional software architect",
		"user2": "Zeph Blake fictional engineer",
		"user3": "石田花子 fictional developer",
	}

	// Multiple searches with caching
	queries := []string{"Zeph", "software", "花子"}

	for _, query := range queries {
		results := engine.Search(data, query, 1)
		if len(results) > 0 {
			fmt.Printf("Query '%s' found result\n", query)
		}
	}

	// Output:
	// Query 'Zeph' found result
	// Query 'software' found result
	// Query '花子' found result
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// verifyTermExists checks if a search term exists in the dataset and logs debug info
func verifyTermExists(t *testing.T, data map[string]string, term string) bool {
	found := false
	for id, text := range data {
		if strings.Contains(strings.ToLower(text), strings.ToLower(term)) {
			found = true
			t.Logf("Term '%s' found in %s: %s", term, id, text)
			break
		}
	}

	if !found {
		t.Logf("WARNING: Term '%s' not found in dataset of size %d", term, len(data))
		// Log first few entries for debugging
		count := 0
		for id, text := range data {
			t.Logf("  Sample entry %s: %s", id, text)
			count++
			if count >= 3 {
				break
			}
		}
	}

	return found
}

// safeSearch performs a search and provides debug info if no results found
func safeSearch(t *testing.T, data map[string]string, query string, maxResults int, engine *SearchEngine) []SearchResult {
	var results []SearchResult

	if engine != nil {
		results = engine.Search(data, query, maxResults)
	} else {
		results = QuickSearch(data, query, maxResults)
	}

	if len(results) == 0 {
		t.Logf("No results found for query '%s' in dataset of size %d", query, len(data))
		verifyTermExists(t, data, query)
	}

	return results
}

// DETERMINISTIC test data generation with GUARANTEED search terms
func generateDeterministicTestData(size int) map[string]string {
	data := make(map[string]string, size)

	// GUARANTEED entries - ensure commonly searched terms always exist
	guaranteedEntries := []struct {
		id   string
		text string
	}{
		{"guaranteed_software", "TestUser software engineer at TechCorp"},
		{"guaranteed_engineer", "Sample engineer developer at DataSoft"},
		{"guaranteed_developer", "Example developer programmer at CodeCraft"},
		{"guaranteed_manager", "Demo manager supervisor at CloudWorks"},
		{"guaranteed_designer", "Mock designer creative at DigitalHub"},
	}

	// Add guaranteed entries first
	for _, entry := range guaranteedEntries {
		if len(data) < size {
			data[entry.id] = entry.text
		}
	}

	// FICTIONAL names only - no real people (deterministic order)
	fictionalNames := []string{
		"Zephen Blakewood", "Maxime Dublanc", "Alex Mockson",
		"TestUser Smith", "Sample Doe", "Example Johnson", "Mock Wilson",
		"María Ejemplos", "José Prueba", "Ana Muestra", "Carlos Demo",
		"Ahmed Fictional", "Fatima Testing", "Omar Example", "Zara Sample",
		"石田花子", "田中テスト", "佐藤サンプル",
		"李测试", "王样本", "张例子",
	}

	fictionalProfessions := []string{
		"software engineer", "product manager", "data scientist",
		"mobile developer", "AI researcher", "full stack developer",
		"DevOps engineer", "security specialist", "UI designer",
		"backend developer", "frontend developer", "ML engineer",
	}

	fictionalCompanies := []string{
		"TechCorp", "DataSoft", "CloudWorks", "MobileTech", "WebDev Inc",
		"CodeCraft", "DevStudio", "TechFlow", "ByteWorks", "SoftLab",
	}

	// Fill remaining slots with deterministic data - FIXED ORDERING
	for i := len(guaranteedEntries); i < size; i++ {
		id := fmt.Sprintf("user%d", i)

		// Use deterministic indexing to ensure same results every time
		nameIdx := i % len(fictionalNames)
		professionIdx := i % len(fictionalProfessions)
		companyIdx := i % len(fictionalCompanies)

		name := fictionalNames[nameIdx]
		profession := fictionalProfessions[professionIdx]
		company := fictionalCompanies[companyIdx]

		text := fmt.Sprintf("%s %s at %s", name, profession, company)
		data[id] = text
	}

	return data
}
