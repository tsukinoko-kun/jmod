package logger

import (
	"fmt"
	"os"
)

var Verbose bool

func Printf(format string, args ...any) {
	if Verbose {
		fmt.Printf(format, args...)
	}
}

func Errorf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format, args...)
}
