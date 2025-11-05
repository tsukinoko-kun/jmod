package statusui

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tsukinoko-kun/disize"
)

// Global state
var (
	program     *tea.Program
	programLock sync.Mutex
)

// Messages for updating the model
type setStatusMsg struct {
	key    string
	status Status
}

type clearStatusMsg struct {
	key string
}

type logMsg struct {
	message string
}

// model holds the state of all status items
type model struct {
	statuses map[string]Status
	keys     []string // Ordered list of keys for consistent rendering
	logs     []string // Log messages to display above status lines
}

func initialModel() model {
	return model{
		statuses: make(map[string]Status),
		keys:     []string{},
		logs:     []string{},
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case setStatusMsg:
		// Update or add status
		m.statuses[msg.key] = msg.status

		// Add key to ordered list if not present
		found := slices.Contains(m.keys, msg.key)
		if !found {
			m.keys = append(m.keys, msg.key)
		}

	case clearStatusMsg:
		// Remove status
		delete(m.statuses, msg.key)

		// Remove key from ordered list
		newKeys := []string{}
		for _, k := range m.keys {
			if k != msg.key {
				newKeys = append(newKeys, k)
			}
		}
		m.keys = newKeys

	case logMsg:
		// Add log message (trim trailing whitespace to avoid extra lines)
		trimmed := strings.TrimRight(msg.message, " \t\n\r")
		if trimmed != "" {
			m.logs = append(m.logs, trimmed)
		}
	}

	return m, nil
}

func (m model) View() string {
	var output strings.Builder

	// Show log messages first (these persist)
	for i, log := range m.logs {
		if i > 0 {
			output.WriteString("\n")
		}
		output.WriteString(log)
	}

	// Show current status items (these are ephemeral)
	for _, key := range m.keys {
		if status, ok := m.statuses[key]; ok {
			if output.Len() > 0 {
				output.WriteString("\n")
			}
			output.WriteString(status.Render())
		}
	}

	return output.String()
}

// Start initializes the Bubbletea program
func Start() error {
	programLock.Lock()
	defer programLock.Unlock()

	if program != nil {
		return fmt.Errorf("TUI already running")
	}

	// Create program for inline rendering (not alternate screen)
	// This will render updates inline in the terminal output
	program = tea.NewProgram(
		initialModel(),
		tea.WithOutput(os.Stderr),
		tea.WithoutSignalHandler(), // Don't intercept signals
		tea.WithInput(nil),         // No input needed
		tea.WithFPS(10),            // Limit to 10 FPS to avoid rapid re-renders
	)

	// Start program in background
	go func() {
		if _, err := program.Run(); err != nil {
			// Log error but don't crash
			fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		}
	}()

	return nil
}

// Stop terminates the Bubbletea program
func Stop() {
	programLock.Lock()
	defer programLock.Unlock()

	if program != nil {
		program.Quit()
		program = nil
		time.Sleep(100 * time.Millisecond)
		fmt.Print(printAfterTuiClose.String())
		printAfterTuiClose.Reset()
	}
}

// Set updates or creates a status for the given key
func Set(key string, status Status) bool {
	programLock.Lock()
	p := program
	programLock.Unlock()

	if p == nil {
		return false
	}

	p.Send(setStatusMsg{key: key, status: status})
	return true
}

// Clear removes a status for the given key
func Clear(key string) {
	programLock.Lock()
	p := program
	programLock.Unlock()

	if p == nil {
		return
	}

	p.Send(clearStatusMsg{key: key})
}

type logLevel uint8

const (
	LogLevelInfo logLevel = iota
	LogLevelWarn
	LogLevelError
)

var (
	logInfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("4"))
	logWarnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("3"))
	logErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1"))
)

var printAfterTuiClose = strings.Builder{}

// Log sends a log message to the TUI
func Log(message string, level logLevel) {
	programLock.Lock()
	p := program
	defer programLock.Unlock()

	var style lipgloss.Style
	switch level {
	case LogLevelInfo:
		style = logInfoStyle
	case LogLevelWarn:
		style = logWarnStyle
	case LogLevelError:
		style = logErrorStyle
	}
	message = style.Render(message)

	if p == nil {
		// If TUI not running, print directly
		fmt.Fprintln(os.Stderr, message)
		return
	}

	printAfterTuiClose.WriteString(message)
	printAfterTuiClose.WriteRune('\n')

	p.Send(logMsg{message: message})
}

// LogWriter is an io.Writer that sends output to the TUI
type LogWriter struct{}

func (lw *LogWriter) Write(p []byte) (n int, err error) {
	if len(p) > 0 {
		Log(string(p), LogLevelInfo)
	}
	return len(p), nil
}

// GetLogWriter returns a writer that sends output to the TUI
func GetLogWriter() io.Writer {
	return &LogWriter{}
}

// ProgressWriter wraps an io.Writer to track progress
type ProgressWriter struct {
	writer    io.Writer
	key       string
	label     string
	total     int64
	current   int64
	lastPrint int64
	mutex     sync.Mutex
}

// NewProgressWriter creates a new ProgressWriter that updates the status UI
func NewProgressWriter(w io.Writer, key, label string, total int64) *ProgressWriter {
	pw := &ProgressWriter{
		writer: w,
		key:    key,
		label:  label,
		total:  total,
	}

	// Set initial progress
	Set(key, ProgressStatus{
		Label:   label,
		Current: 0,
		Total:   total,
	})

	return pw
}

func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n, err := pw.writer.Write(p)

	pw.mutex.Lock()
	pw.current += int64(n)

	// Update status every 100KB or at completion
	shouldUpdate := pw.current-pw.lastPrint >= 100*disize.Kib || pw.current == pw.total
	if shouldUpdate {
		pw.lastPrint = pw.current
		Set(pw.key, ProgressStatus{
			Label:   pw.label,
			Current: pw.current,
			Total:   pw.total,
		})
	}
	pw.mutex.Unlock()

	return n, err
}
