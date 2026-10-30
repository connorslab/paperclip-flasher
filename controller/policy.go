package main

import (
	"errors"
	"fmt"
	"math"
)

type Tuning struct {
	Millivolts int    `json:"millivolts"`
	Clock      int    `json:"clock"`
	Fan        int    `json:"fan"`
	Mode       string `json:"mode"`
}

func (t Tuning) Validate(previous Tuning) error {
	if t.Millivolts < 400 || t.Millivolts > 430 || t.Millivolts%5 != 0 {
		return errors.New("voltage must be 400–430 mV in 5 mV steps; above-stock voltage is disabled")
	}
	if t.Millivolts-previous.Millivolts > 5 || previous.Millivolts-t.Millivolts > 5 {
		return errors.New("change voltage by at most 5 mV per trial")
	}
	if t.Clock < 500 || t.Clock > 800 || t.Clock%5 != 0 {
		return errors.New("clock must be 500–800 MHz in 5 MHz steps")
	}
	if t.Clock > previous.Clock+25 {
		return errors.New("increase by at most 25 MHz per trial")
	}
	if t.Fan < 80 || t.Fan > 100 {
		return errors.New("fan floor must be 80–100%")
	}
	if t.Mode != "fixed" && t.Mode != "curve" {
		return errors.New("unknown fan mode")
	}
	return nil
}
func (t Tuning) FanAt(temp float64) int {
	if t.Mode == "fixed" {
		return t.Fan
	}
	return int(math.Min(100, math.Max(float64(t.Fan), float64(t.Fan)+(temp-60)*(100-float64(t.Fan))/20)))
}
func (t Tuning) Powerplan() string {
	return fmt.Sprintf("%d MHz %.3f V %d RPM %d RPM", t.Clock, float64(t.Millivolts)/1000, t.Fan, t.Fan)
}

// Counter deltas measure invalid nonces, not errors divided by pool shares.
func errorRate(oldHW, oldNonces, hw, nonces float64) (float64, bool) {
	if hw < oldHW || nonces <= oldNonces {
		return 0, false
	}
	return 100 * (hw - oldHW) / (nonces - oldNonces), true
}
