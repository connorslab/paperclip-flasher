package main

import (
	"errors"
	"testing"
	"time"
)

func TestFanStartupRecheck(t *testing.T) {
	for _, tc := range []struct {
		name        string
		startup     bool
		first, next int
		err         error
		want        bool
		reads       int
	}{
		{"healthy", true, 4700, 4700, nil, true, 1},
		{"spinup", true, 0, 4700, nil, true, 2},
		{"stalled", true, 0, 0, nil, false, 2},
		{"sensor-error", true, 4700, 4700, errors.New("sensor"), false, 2},
		{"runtime-fault", false, 0, 4700, nil, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, waits := 0, 0
			got := fanReady(func() (int, error) {
				reads++
				if reads == 1 {
					return tc.first, tc.err
				}
				return tc.next, tc.err
			}, tc.startup, func(d time.Duration) {
				waits++
				if d != 2*time.Second {
					t.Fatal(d)
				}
			})
			if got != tc.want || reads != tc.reads || waits != tc.reads-1 {
				t.Fatalf("ready=%v reads=%d waits=%d", got, reads, waits)
			}
		})
	}
}
