package main

import "time"

// A cold engine can report RPM before the commanded fan speed has settled.
// Allow one two-second recheck during startup only; persistent faults still stop.
func fanReady(read func() (int, error), startup bool, wait func(time.Duration)) bool {
	rpm, err := read()
	if err == nil && rpm >= 3000 {
		return true
	}
	if !startup {
		return false
	}
	wait(2 * time.Second)
	rpm, err = read()
	return err == nil && rpm >= 3000
}
