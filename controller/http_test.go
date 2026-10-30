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
