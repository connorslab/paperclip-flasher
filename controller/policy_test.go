package main

import "testing"

func TestBounds(t *testing.T) {
	base := Tuning{430, 740, 100, "fixed"}
	for _, v := range []Tuning{{430, 800, 100, "fixed"}, {430, 745, 79, "fixed"}, {430, 850, 100, "fixed"}, {430, 741, 100, "fixed"}, {430, 745, 100, "bad"}} {
		if v.Validate(base) == nil {
			t.Fatalf("accepted unsafe trial %#v", v)
		}
	}
	if (Tuning{430, 725, 85, "curve"}).Validate(base) != nil {
		t.Fatal("valid trial rejected")
	}
}
func TestCurve(t *testing.T) {
	v := Tuning{430, 740, 80, "curve"}
	for _, c := range []struct {
		temp float64
		want int
	}{{30, 80}, {60, 80}, {70, 90}, {80, 100}, {90, 100}} {
		if v.FanAt(c.temp) != c.want {
			t.Fatal(c)
		}
	}
}
func TestRateReset(t *testing.T) {
	if _, ok := errorRate(20, 2000, 2, 200); ok {
		t.Fatal("reset misreported")
	}
	r, ok := errorRate(20, 2000, 22, 4000)
	if !ok || r != 0.1 {
		t.Fatal(r, ok)
	}
}

func TestVoltageBounds(t *testing.T) {
	base := Tuning{430, 740, 100, "fixed"}
	for _, mv := range []int{0, 395, 400, 420, 426, 435, 450} {
		v := base
		v.Millivolts = mv
		if v.Validate(base) == nil {
			t.Fatalf("unsafe voltage step accepted: %d", mv)
		}
	}
	v := base
	v.Millivolts = 425
	if e := v.Validate(base); e != nil {
		t.Fatal(e)
	}
	if v.Powerplan() != "740 MHz 0.425 V 100 RPM 100 RPM" {
		t.Fatal(v.Powerplan())
	}
}
