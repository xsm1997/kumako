package main

import "fmt"

const (
	DebugLevelFatal = 0
	DebugLevelError = 1
	DebugLevelInfo = 2
	DebugLevelDebug = 3
)

func LogD(tag string, format string, args ...interface{}) {
	if config.DebugLevel >= DebugLevelDebug {
		fmt.Printf("[%s][DEBUG] ", tag)
		fmt.Printf(format, args...)
		fmt.Printf("\n")
	}
}

func LogI(tag string, format string, args ...interface{}) {
	if config.DebugLevel >= DebugLevelInfo {
		fmt.Printf("[%s][INFO] ", tag)
		fmt.Printf(format, args...)
		fmt.Printf("\n")
	}
}

func LogE(tag string, format string, args ...interface{}) {
	if config.DebugLevel >= DebugLevelError {
		fmt.Printf("[%s][ERROR] ", tag)
		fmt.Printf(format, args...)
		fmt.Printf("\n")
	}
}

func LogF(tag string, format string, args ...interface{}) {
	if config.DebugLevel >= DebugLevelFatal {
		fmt.Printf("[%s][FATAL] ", tag)
		fmt.Printf(format, args...)
		fmt.Printf("\n")
	}
}
