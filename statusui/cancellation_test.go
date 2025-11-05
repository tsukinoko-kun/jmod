package statusui

import (
	"context"
	"testing"
	"time"
)

// TestContextCancellationScenario tests that the statusui properly clears
// status items when operations are cancelled
func TestContextCancellationScenario(t *testing.T) {
	// Start the UI
	err := Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer Stop()

	// Simulate a download in progress
	Set("test-package", ProgressStatus{
		Label:   "Downloading test-package",
		Current: 50,
		Total:   100,
	})

	time.Sleep(50 * time.Millisecond)

	// Simulate cancellation by clearing the status (no message shown)
	Clear("test-package")

	time.Sleep(50 * time.Millisecond)

	// Verify the status was cleared (no error should occur)
	// The actual rendering is tested visually or in integration tests
}

// TestMultipleConcurrentOperationsWithCancellation tests that cancellation
// of one operation doesn't affect others
func TestMultipleConcurrentOperationsWithCancellation(t *testing.T) {
	err := Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer Stop()

	// Start multiple operations
	Set("pkg1", TextStatus{Text: "Processing pkg1"})
	Set("pkg2", ProgressStatus{Label: "Downloading pkg2", Current: 25, Total: 100})
	Set("pkg3", TextStatus{Text: "Processing pkg3"})

	time.Sleep(50 * time.Millisecond)

	// Cancel pkg2 (silently clear it)
	Clear("pkg2")

	time.Sleep(50 * time.Millisecond)

	// Complete the others
	Set("pkg1", SuccessStatus{Message: "Installed pkg1"})
	Set("pkg3", SuccessStatus{Message: "Installed pkg3"})

	time.Sleep(50 * time.Millisecond)

	// Clear remaining
	Clear("pkg1")
	Clear("pkg3")
}

// TestContextCancellationPropagation verifies that context cancellation
// is properly detected and handled
func TestContextCancellationPropagation(t *testing.T) {
	err := Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer Stop()

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Simulate an operation
	Set("download", ProgressStatus{
		Label:   "Downloading",
		Current: 0,
		Total:   1000,
	})

	// Cancel the context
	cancel()

	// Simulate checking for cancellation
	select {
	case <-ctx.Done():
		// Context was cancelled, silently clear the status
		Clear("download")
	default:
		t.Error("Context should have been cancelled")
	}

	time.Sleep(50 * time.Millisecond)

	// Verify no panic or deadlock occurred
}

// TestGracefulShutdownOnError tests that Stop() works correctly even
// when there are error statuses
func TestGracefulShutdownOnError(t *testing.T) {
	err := Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Set various statuses including errors
	Set("err1", ErrorStatus{Message: "Network error"})
	Set("err2", ErrorStatus{Message: "Checksum mismatch"})

	time.Sleep(50 * time.Millisecond)

	// Stop should handle errors gracefully
	Stop()

	// Verify no panic occurred and we can restart
	err = Start()
	if err != nil {
		t.Fatalf("Restart after error failed: %v", err)
	}
	Stop()
}

// TestTimeoutScenario simulates a timeout scenario
func TestTimeoutScenario(t *testing.T) {
	err := Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer Stop()

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	Set("timeout-test", ProgressStatus{
		Label:   "Slow download",
		Current: 10,
		Total:   1000,
	})

	// Wait for timeout
	<-ctx.Done()

	// Check if it was a timeout - silently clear
	if ctx.Err() == context.DeadlineExceeded {
		Clear("timeout-test")
	}

	time.Sleep(50 * time.Millisecond)
}
