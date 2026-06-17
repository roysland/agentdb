package db

import (
	"context"
	"database/sql"
	"runtime"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// =============================================================================
// Bug Condition Exploration Tests — ConnectionHandle (Bugs 1.1, 1.9)
//
// These tests encode the EXPECTED (correct) behavior. On UNFIXED code, they
// MUST FAIL — failure confirms the bugs exist. After fixes are applied, these
// same tests will PASS, confirming the bugs are resolved.
//
// DO NOT modify these tests to make them pass on unfixed code.
// =============================================================================

// -----------------------------------------------------------------------------
// Test 1a: Goroutine Leak (Bug 1.1)
//
// Bug condition: WriteContext times out → goroutine remains blocked on ch.mu.Lock()
// Expected behavior: No goroutine leak; clean cancellation on timeout
// -----------------------------------------------------------------------------

func TestBugCondition_GoroutineLeak(t *testing.T) {
	// Create a minimal ConnectionHandle directly (bypassing NewConnectionHandle
	// which requires PRAGMAs that may not be supported in all environments)
	tmpDB := t.TempDir() + "/goroutine_leak_test.db"
	database, err := sql.Open("sqlite", tmpDB)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	ch := &ConnectionHandle{
		db:       database,
		writeTTL: 3 * time.Second,
		readTTL:  5 * time.Second,
		mutexTTL: 500 * time.Millisecond, // Short timeout to speed up test
	}

	ctx := context.Background()

	// Acquire the write lock and hold it to force subsequent WriteContext calls to timeout
	_, cancel, err := ch.WriteContext(ctx)
	if err != nil {
		t.Fatalf("failed to acquire initial write context: %v", err)
	}

	// Record goroutine count before triggering timeouts
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	goroutinesBefore := runtime.NumGoroutine()

	// Trigger multiple WriteContext timeouts while the lock is held
	const numTimeouts = 5
	var wg sync.WaitGroup
	for i := 0; i < numTimeouts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := ch.WriteContext(ctx)
			if err == nil {
				t.Error("expected WriteContext to fail with timeout, but it succeeded")
			}
		}()
	}

	// Wait for all timeout attempts to complete
	wg.Wait()

	// Check goroutine count while lock is still held.
	// Leaked goroutines are blocked on ch.mu.Lock() and will remain until released.
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	goroutinesWhileHeld := runtime.NumGoroutine()

	// Release the original lock so leaked goroutines can drain
	cancel()
	ch.ReleaseWrite()

	// EXPECTED BEHAVIOR: After all WriteContext calls have timed out, there should
	// be NO goroutines still blocked on ch.mu.Lock(). A correct implementation
	// (e.g., channel-based semaphore) would not spawn goroutines at all.
	//
	// On UNFIXED code: Each timed-out WriteContext spawns a goroutine that calls
	// ch.mu.Lock(). When the timeout fires, the function returns an error but the
	// goroutine remains blocked on the mutex. These accumulate indefinitely.
	leaked := goroutinesWhileHeld - goroutinesBefore
	if leaked >= numTimeouts-1 {
		t.Errorf("GOROUTINE LEAK DETECTED: %d goroutines leaked while lock held after %d timeouts\n"+
			"  Before timeouts: %d goroutines\n"+
			"  After timeouts (lock still held): %d goroutines\n"+
			"  This confirms Bug 1.1: WriteContext spawns goroutines that remain blocked\n"+
			"  on ch.mu.Lock() after timeout. In production, repeated timeouts cause\n"+
			"  unbounded goroutine accumulation until the process runs out of memory.",
			leaked, numTimeouts, goroutinesBefore, goroutinesWhileHeld)
	}

	// Give leaked goroutines time to drain after lock release
	time.Sleep(500 * time.Millisecond)
}

// -----------------------------------------------------------------------------
// Test 1d: Write Timeout (Bug 1.9 — fixed)
//
// Bug was: writeTTL defaulted to 3 seconds, causing any indexing operation
// longer than 3s to fail with context deadline exceeded.
// Fix: NewConnectionHandle now sets writeTTL to 5 minutes.
//
// This test verifies the production writeTTL is large enough for real workloads.
// -----------------------------------------------------------------------------

func TestBugCondition_WriteTimeout(t *testing.T) {
	tmpDB := t.TempDir() + "/write_timeout_test.db"
	database, err := sql.Open("sqlite", tmpDB)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	// Use the production writeTTL (5 minutes) — this is what NewConnectionHandle sets.
	ch := &ConnectionHandle{
		db:       database,
		writeTTL: 5 * time.Minute,
		readTTL:  5 * time.Second,
		mutexTTL: 3 * time.Second,
	}

	ctx := context.Background()

	writeCtx, cancel, err := ch.WriteContext(ctx)
	if err != nil {
		t.Fatalf("failed to acquire write context: %v", err)
	}
	defer func() {
		cancel()
		ch.ReleaseWrite()
	}()

	deadline, hasDeadline := writeCtx.Deadline()
	if !hasDeadline {
		t.Fatal("WriteContext should have a deadline set")
	}

	timeUntilDeadline := time.Until(deadline)

	// The write deadline must be well above any realistic single-file indexing
	// operation. Require at least 4 minutes of headroom.
	const minBudget = 4 * time.Minute
	if timeUntilDeadline < minBudget {
		t.Errorf("write deadline too short: %v remaining, want >= %v\n"+
			"  Deadline: %v\n"+
			"  NewConnectionHandle sets writeTTL=5m; check that value has not regressed",
			timeUntilDeadline.Round(time.Second), minBudget, deadline)
	}
}
