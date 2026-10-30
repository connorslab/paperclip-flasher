package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"
)

// This read-only local RPC provides the running clock, rather than saved settings.
func minerRPC(command string) (map[string]json.RawMessage, error) {
	c, e := net.DialTimeout("tcp", "127.0.0.1:4028", 2*time.Second)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, e = c.Write([]byte(`{"command":"` + command + `"}`)); e != nil {
		return nil, e
	}
	b, e := io.ReadAll(io.LimitReader(c, 128*1024))
	if e != nil {
		return nil, e
	}
	b = bytes.TrimRight(b, "\x00\r\n")
	var v map[string]json.RawMessage
	if e = json.Unmarshal(b, &v); e != nil {
		return nil, e
	}
	return v, nil
}

func liveMiner() (map[string]any, error) {
	v, e := minerRPC("devs")
	if e != nil {
		return nil, e
	}
	for _, key := range []string{"DEVS", "PGA"} {
		var a []map[string]any
		if json.Unmarshal(v[key], &a) == nil && len(a) == 1 {
			return a[0], nil
		}
	}
	return nil, errors.New("missing live miner telemetry")
}
