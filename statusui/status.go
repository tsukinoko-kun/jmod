package statusui

import "fmt"

// Status represents a displayable status item in the TUI
type Status interface {
	// Render returns the string representation of the status
	Render() string
}

// TextStatus displays plain text
type TextStatus struct {
	Text string
}

func (t TextStatus) Render() string {
	return t.Text
}

// ProgressStatus displays a progress bar
type ProgressStatus struct {
	Label   string
	Current int64
	Total   int64
}

func (p ProgressStatus) Render() string {
	if p.Total <= 0 {
		return fmt.Sprintf("%s: %s", p.Label, formatBytes(p.Current))
	}
	percentage := float64(p.Current) / float64(p.Total) * 100
	barWidth := 30
	filled := int(float64(barWidth) * float64(p.Current) / float64(p.Total))
	if filled > barWidth {
		filled = barWidth
	}
	
	bar := ""
	for i := 0; i < barWidth; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	
	return fmt.Sprintf("%s: [%s] %.1f%% (%s/%s)", 
		p.Label, bar, percentage, formatBytes(p.Current), formatBytes(p.Total))
}

// ErrorStatus displays an error message
type ErrorStatus struct {
	Message string
	Err     error
}

func (e ErrorStatus) Render() string {
	if e.Err != nil {
		return fmt.Sprintf("❌ %s: %v", e.Message, e.Err)
	}
	return fmt.Sprintf("❌ %s", e.Message)
}

// SuccessStatus displays a success message
type SuccessStatus struct {
	Message string
}

func (s SuccessStatus) Render() string {
	return fmt.Sprintf("✓ %s", s.Message)
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

