package logger

import (
	"fmt"

	"github.com/tsukinoko-kun/jmod/statusui"
)

var Verbose bool

func Printf(format string, args ...any) {
	if Verbose {
		statusui.Log(fmt.Sprintf(format, args...), statusui.LogLevelInfo)
	}
}

func Errorf(format string, args ...any) {
	statusui.Log(fmt.Sprintf(format, args...), statusui.LogLevelError)
}
