package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHTTPWriteProtection(t *testing.T) {
	c := &Controller{sessions: map[string]Session{}, lastGood: time.Now()}
	for _, v := range []struct {
		origin, ctype string
		want          int
	}{{"http://evil.example", "application/json", 403}, {"", "text/plain", 415}, {"", "application/json", 401}} {
		r := httptest.NewRequest("POST", "http://miner/api/trial", strings.NewReader(`{"millivolts":430,"clock":745,"fan":100,"mode":"fixed"}`))
		r.Header.Set("Origin", v.origin)
		r.Header.Set("Content-Type", v.ctype)
		w := httptest.NewRecorder()
		c.handler(w, r)
		if w.Code != v.want {
			t.Fatalf("got %d want %d", w.Code, v.want)
		}
	}
}
func TestTrialJournalPrecedesApplyAndRollback(t *testing.T) {
	dir := t.TempDir()
	puts := 0
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/login":
			w.Write([]byte(`{"JWT Token":"test"}`))
		case "/mcb/setting":
			if r.Method == "PUT" {
				puts++
				if _, e := os.Stat(filepath.Join(dir, "pending.json")); e != nil {
					t.Error("settings applied without durable journal")
				}
			}
			w.Write([]byte(`{"tempcontrol":true}`))
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer native.Close()
	c := &Controller{dir: dir, backend: native.URL, client: native.Client(), approved: Tuning{430, 740, 100, "fixed"}, sessions: map[string]Session{"test": {time.Now().Add(time.Minute)}}, lastGood: time.Now(), device: map[string]any{"nonces": float64(100)}}
	r := httptest.NewRequest("POST", "http://miner/api/trial", strings.NewReader(`{"millivolts":430,"clock":725,"fan":100,"mode":"fixed"}`))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "paperclip", Value: "test"})
	w := httptest.NewRecorder()
	c.handler(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if c.pending == nil || puts != 1 {
		t.Fatal("trial missing")
	}
	if e := c.rollback("test"); e != nil {
		t.Fatal(e)
	}
	if puts != 2 || c.pending != nil || c.approved.Clock != 740 {
		t.Fatal("rollback incorrect")
	}
	b, e := os.ReadFile(filepath.Join(dir, "approved.json"))
	var restored Tuning
	if e != nil || json.Unmarshal(b, &restored) != nil || restored.Clock != 740 {
		t.Fatal("rollback not durable")
	}
	if _, e = os.Stat(filepath.Join(dir, "pending.json")); !os.IsNotExist(e) {
		t.Fatal("journal not cleared")
	}
}
func TestTemperature(t *testing.T) {
	for _, v := range []string{"", "unavailable", "0 C"} {
		if _, ok := temperature(v); ok {
			t.Fatal(v)
		}
	}
	v, ok := temperature("76.0 °C / 54.0 °C")
	if !ok || v != 76 {
		t.Fatal(v)
	}
}

func TestBootRollbackRetriesWithoutLatchingFault(t *testing.T) {
	dir := t.TempDir()
	failed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/login" {
			w.Write([]byte(`{"JWT Token":"test"}`))
			return
		}
		if r.Method == "PUT" && !failed {
			failed = true
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"manualPowerplan":"740 MHz 0.430 V 100 RPM 100 RPM"}`))
	}))
	defer server.Close()
	previous := Tuning{430, 740, 100, "fixed"}
	pending := Pending{previous, Tuning{425, 725, 100, "fixed"}}
	if e := atomicJSON(filepath.Join(dir, "pending.json"), pending); e != nil {
		t.Fatal(e)
	}
	c := &Controller{dir: dir, backend: server.URL, client: server.Client(), pending: &pending, recovering: true}
	if c.rollback("boot") == nil {
		t.Fatal("expected transient failure")
	}
	if c.halted || c.pending == nil {
		t.Fatal("transient startup failure latched a stop or discarded recovery")
	}
	if e := c.rollback("boot"); e != nil {
		t.Fatal(e)
	}
	if c.halted || c.pending != nil || c.approved != previous {
		t.Fatal("boot retry failed")
	}
}

// Confirmation must not race the periodic safety loop after the deadline.
func TestExpiredTrialCannotBeConfirmed(t *testing.T) {
	dir := t.TempDir()
	old := Tuning{430, 600, 100, "fixed"}
	c := &Controller{dir: dir, approved: old, pending: &Pending{old, Tuning{430, 625, 100, "fixed"}},
		sessions: map[string]Session{"test": {time.Now().Add(time.Minute)}},
		lastGood: time.Now(), started: time.Now().Add(-4 * time.Minute), deadline: time.Now().Add(-time.Second),
		samples: 20, baselineAccepted: 1, device: map[string]any{"accepted": float64(2)}}
	req := httptest.NewRequest("POST", "http://miner/api/confirm", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "paperclip", Value: "test"})
	w := httptest.NewRecorder()
	c.handler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expired trial confirmed: %d", w.Code)
	}
	if c.approved != old || c.pending == nil {
		t.Fatal("expired confirmation changed approved settings")
	}
	if _, err := os.Stat(filepath.Join(dir, "approved.json")); !os.IsNotExist(err) {
		t.Fatal("expired confirmation wrote settings")
	}
}

func TestChipTelemetryRefreshAndMissingSensors(t *testing.T) {
	body := `{"drawdata":[[{"temp":72},{"temp":79}]]}`
	logins := 0
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/login" {
			logins++
			w.Write([]byte(`{"JWT Token":"fresh"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"body": body})
	}))
	defer native.Close()
	c := &Controller{backend: native.URL, client: native.Client(), token: "expired"}
	peak, err := c.chipPeak()
	if err != nil || peak != 79 || logins != 1 {
		t.Fatalf("peak=%v err=%v logins=%d", peak, err, logins)
	}
	for _, bad := range []string{`{"drawdata":[]}`, `{"drawdata":[[{}]]}`, `{"drawdata":[[{"temp":0}]]}`, `not-json`} {
		body = bad
		if _, err = c.chipPeak(); err == nil {
			t.Fatalf("accepted invalid telemetry: %s", bad)
		}
	}
}

func TestDiagnosticsUsesNativeAuthentication(t *testing.T) {
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/login" {
			w.Write([]byte(`{"JWT Token":"fresh"}`))
			return
		}
		if r.URL.Path != "/dbg/icinfo" || r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"body":"chip temperatures"}`))
	}))
	defer native.Close()
	c := &Controller{backend: native.URL, client: native.Client(), sessions: map[string]Session{"test": {time.Now().Add(time.Minute)}}}
	r := httptest.NewRequest("GET", "http://miner/api/diagnostics?kind=icinfo", nil)
	r.AddCookie(&http.Cookie{Name: "paperclip", Value: "test"})
	w := httptest.NewRecorder()
	c.handler(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "chip temperatures") {
		t.Fatal(w.Code, w.Body.String())
	}
}
