package db

import (
	"context"
	"database/sql"
	"runtime"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
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
	database, err := sql.Open("sqlite3", tmpDB)
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
// Test 1d: Write Timeout (Bug 1.9)
//
// Bug condition: writeTTL is 3 seconds, any operation >3s fails
// Expected behavior: Long-running operations complete without premature timeout
// -----------------------------------------------------------------------------

func TestBugCondition_WriteTimeout(t *testing.T) {
	// Create a minimal ConnectionHandle directly
	tmpDB := t.TempDir() + "/write_timeout_test.db"
	database, err := sql.Open("sqlite3", tmpDB)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	ch := &ConnectionHandle{
		db:       database,
		writeTTL: 3 * time.Second, // This is the buggy default
		readTTL:  5 * time.Second,
		mutexTTL: 3 * time.Second,
	}

	ctx := context.Background()

	// Acquire a write context — this gives us a context with the writeTTL deadline
	writeCtx, cancel, err := ch.WriteContext(ctx)
	if err != nil {
		t.Fatalf("failed to acquire write context: %v", err)
	}
	defer func() {
		cancel()
		ch.ReleaseWrite()
	}()

	// Check the deadline on the write context
	deadline, hasDeadline := writeCtx.Deadline()
	if !hasDeadline {
		t.Fatal("WriteContext should have a deadline set")
	}

	timeUntilDeadline := time.Until(deadline)

	// EXPECTED BEHAVIOR: The write timeout should be long enough for indexing
	// operations (e.g., 5 minutes). A 3-second timeout guarantees failure for
	// any non-trivial repository indexing.
	//
	// On UNFIXED code: writeTTL is 3 seconds, which is far too short for
	// indexing/analysis operations that may take minutes.
	if timeUntilDeadline <= 4*time.Second {
		t.Errorf("WRITE TIMEOUT TOO SHORT: writeTTL is approximately %v\n"+
			"  Deadline: %v\n"+
			"  Time until deadline: %v\n"+
			"  Any indexing operation taking >3s will fail with context deadline exceeded\n"+
			"  This confirms Bug 1.9: 3-second writeTTL is insufficient for real workloads\n"+
			"  Expected: timeout should be >= 5 minutes for indexing operations",
			timeUntilDeadline.Round(time.Millisecond),
			deadline,
			timeUntilDeadline.Round(time.Millisecond))
	}

	// Also verify that a simulated long operation would fail
	simulatedOpDuration := 3500 * time.Millisecond
	if timeUntilDeadline < simulatedOpDuration {
		t.Logf("  Confirmed: A %v operation would exceed the %v deadline",
			simulatedOpDuration, timeUntilDeadline.Round(time.Millisecond))
	}
}
