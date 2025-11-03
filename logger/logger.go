package logger

import "fmt"

var Verbose bool

func Printf(format string, args ...any) {
	if Verbose {
		fmt.Printf(format, args...)
	}
}
