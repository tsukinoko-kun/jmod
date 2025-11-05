package statusui

import (
	"strings"
	"testing"
	"time"
)

func TestStatusUI(t *testing.T) {
	// Test Start/Stop
	err := Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Test Set with different status types
	Set("test1", TextStatus{Text: "Simple text status"})
	Set("test2", ProgressStatus{Label: "Downloading", Current: 50, Total: 100})
	Set("test3", ErrorStatus{Message: "Test error"})
	Set("test4", SuccessStatus{Message: "Test success"})

	// Give time for rendering
	time.Sleep(100 * time.Millisecond)

	// Test Clear
	Clear("test1")

	// Give time for rendering
	time.Sleep(100 * time.Millisecond)

	// Test Stop
	Stop()

	// Test that operations are no-op after stop
	Set("test5", TextStatus{Text: "Should not appear"})
	Clear("test5")
	Stop() // Should be no-op
}

func TestStatusUINotInitialized(t *testing.T) {
	// Ensure no instance is running
	Stop()

	// These should all be no-ops
	Set("test", TextStatus{Text: "Should be no-op"})
	Clear("test")
	Stop()

	// No errors should occur
}

func TestProgressStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   ProgressStatus
		contains string
	}{
		{
			name:     "with total",
			status:   ProgressStatus{Label: "Download", Current: 512000, Total: 1024000},
			contains: "Download",
		},
		{
			name:     "without total",
			status:   ProgressStatus{Label: "Upload", Current: 1024},
			contains: "Upload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.status.Render()
			if result == "" {
				t.Error("Expected non-empty render result")
			}
			if len(result) > 0 && !strings.Contains(result, tt.contains) {
				t.Errorf("Expected render to contain %q, got %q", tt.contains, result)
			}
		})
	}
}

func TestTextStatus(t *testing.T) {
	status := TextStatus{Text: "Hello World"}
	result := status.Render()
	if result != "Hello World" {
		t.Errorf("Expected %q, got %q", "Hello World", result)
	}
}

func TestErrorStatus(t *testing.T) {
	status := ErrorStatus{Message: "Failed", Err: nil}
	result := status.Render()
	if !strings.Contains(result, "Failed") {
		t.Errorf("Expected render to contain 'Failed', got %q", result)
	}
}

func TestSuccessStatus(t *testing.T) {
	status := SuccessStatus{Message: "Done"}
	result := status.Render()
	if !strings.Contains(result, "Done") {
		t.Errorf("Expected render to contain 'Done', got %q", result)
	}
}
