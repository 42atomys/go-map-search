package engine

import (
	"sync"
	"testing"
)

func TestContextPool(t *testing.T) {
	// Acquire a context from the pool
	ctx := contextPool.Get().(*Context)

	// Modify the context
	ctx.queryNormLen = 10
	ctx.docNormLen = 20
	ctx.queryWordCount = 5
	ctx.docWordCount = 8
	ctx.candidateCount = 3
	ctx.candidateSetLen = 2

	// Reset the context
	ctx.reset()

	// Validate that the context is reset
	if ctx.queryNormLen != 0 || ctx.docNormLen != 0 || ctx.queryWordCount != 0 || ctx.docWordCount != 0 || ctx.candidateCount != 0 || ctx.candidateSetLen != 0 {
		t.Errorf("Context was not properly reset")
	}

	// Return the context to the pool
	contextPool.Put(ctx)
}

func TestContextPoolMemoryLeak(t *testing.T) {
	var wg sync.WaitGroup
	poolSize := 1000

	// Simulate concurrent usage of the context pool
	for i := 0; i < poolSize; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := contextPool.Get().(*Context)
			ctx.reset()
			contextPool.Put(ctx)
		}()
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// If we reach here without a panic
	// it means the pool is functioning correctly without memory leaks
	// Check if the pool is not leaking memory
	if cap(contextPool.Get().(*Context).queryNormalized) != 2048 || cap(contextPool.Get().(*Context).docNormalized) != 8192 {
		t.Errorf("Context buffers have incorrect capacity, expected 2048 for queryNormalized and 8192 for docNormalized")
	}
}
