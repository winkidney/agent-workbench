//go:build !linux

package workbench

import "time"

// Outside Linux the extra processes are listed by number only.
func processParent(int) int                { return 0 }
func processCommand(int) string            { return "" }
func processAge(int) (time.Duration, bool) { return 0, false }
