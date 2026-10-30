package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func allowedOrigin(origin, host string) bool {
	u, e := url.Parse(origin)
	h := strings.Split(host, ":")[0]
	return e == nil && u.Scheme == "http" && u.Hostname() == h && (u.Port() == "" || u.Port() == "80" || u.Port() == "8088") && u.Path == "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

type NetworkTrial struct {
	Previous json.RawMessage `json:"previous"`
	Deadline time.Time       `json:"deadline"`
}
type NetworkSettings struct {
	DHCP bool              `json:"useDHCP"`
	Auto bool              `json:"useAuto"`
	IP   map[string]string `json:"setIP"`
}

func (n NetworkSettings) validate() error {
	for k, v := range n.IP {
		if k != "IPv4addr" && k != "Subnetmask" && k != "Router" && k != "DNS1" && k != "DNS2" {
			return errors.New("unknown network field")
		}
		if v != "" && (net.ParseIP(v) == nil || net.ParseIP(v).To4() == nil) {
			return errors.New("enter valid IPv4 addresses")
		}
	}
	if !n.DHCP {
		for _, k := range []string{"IPv4addr", "Subnetmask", "Router"} {
			if n.IP[k] == "" {
				return errors.New("static IP, mask and gateway are required")
			}
		}
		mask := net.IPMask(net.ParseIP(n.IP["Subnetmask"]).To4())
		ones, bits := mask.Size()
		if bits != 32 || ones == 0 {
			return errors.New("invalid subnet mask")
		}
	}
	if !n.Auto && n.IP["DNS1"] == "" {
		return errors.New("DNS 1 is required when automatic DNS is off")
	}
	return nil
}
func (c *Controller) restoreNetwork() error {
	if c.networkPending == nil {
		return nil
	}
	token, e := c.login(c.cfg.AdminPassword)
	if e != nil {
		return e
	}
	c.token = token
	if _, e = c.native("PUT", "/mcb/ip", c.networkPending.Previous, true); e != nil {
		return e
	}
	if e = os.Remove(filepath.Join(c.dir, "network-pending.json")); e != nil {
		return e
	}
	c.networkPending = nil
	c.message = "Previous network configuration restored"
	return nil
}
func (c *Controller) settings(w http.ResponseWriter, r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	if path == "diagnostics" && r.Method == "GET" {
		name := r.URL.Query().Get("kind")
		allowed := map[string]bool{"kmsg": true, "syslog": true, "psinfo": true, "meminfo": true, "vsinfo": true, "minerinfo": true, "minersyslog": true, "monitorlog": true, "icinfo": true, "minerhistory": true}
		if !allowed[name] {
			jsonReply(w, 400, map[string]string{"error": "unknown diagnostic"})
			return true
		}
		res, e := c.client.Get(c.backend + "/dbg/" + name)
		if e != nil {
			jsonReply(w, 502, map[string]string{"error": "diagnostics unavailable"})
			return true
		}
		defer res.Body.Close()
		var v struct {
			Body string `json:"body"`
		}
		e = json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(&v)
		if e != nil {
			jsonReply(w, 502, map[string]string{"error": "diagnostic exceeds safe read limit"})
		} else {
			if len(v.Body) > 65536 {
				v.Body = v.Body[len(v.Body)-65536:]
			}
			jsonReply(w, 200, map[string]string{"body": v.Body})
		}
		return true
	}
	reads := map[string]string{"network": "/mcb/ip", "wifi": "/mcb/wifisetting", "pools": "/mcb/pools", "system": "/mcb/status", "cloud": "/mcb/cmossetting", "rgb": "/mcb/rgbsetting", "device-settings": "/mcb/setting"}
	if endpoint, ok := reads[path]; ok && r.Method == "GET" {
		b, e := c.native("GET", endpoint, nil, false)
		if e != nil {
			jsonReply(w, 502, map[string]string{"error": "Device settings unavailable"})
		} else {
			jsonReply(w, 200, json.RawMessage(b))
		}
		return true
	}
	if path == "network-trial" && r.Method == "GET" {
		jsonReply(w, 200, c.networkPending)
		return true
	}
	if r.Method != "POST" {
		return false
	}
	var body json.RawMessage
	switch path {
	case "network", "network-confirm", "wifi", "pool-add", "pool-delete", "pools", "cloud", "rgb", "identify", "password", "reboot", "logout":
	default:
		return false
	}
	if e := decode(r, &body); e != nil {
		jsonReply(w, 400, map[string]string{"error": "Invalid JSON"})
		return true
	}
	if c.pending != nil {
		jsonReply(w, 409, map[string]string{"error": "Finish or revert the tuning trial first"})
		return true
	}
	token, e := c.login(c.cfg.AdminPassword)
	if e != nil {
		jsonReply(w, 502, map[string]string{"error": "Controller authentication failed"})
		return true
	}
	c.token = token
	switch path {
	case "network":
		var n NetworkSettings
		e = json.Unmarshal(body, &n)
		if e == nil {
			e = n.validate()
		}
		if e == nil && c.networkPending != nil {
			e = errors.New("confirm or wait for the current network trial")
		}
		if e == nil {
			var previous []byte
			previous, e = c.native("GET", "/mcb/ip", nil, false)
			if e == nil {
				p := &NetworkTrial{previous, time.Now().Add(2 * time.Minute)}
				e = atomicJSON(filepath.Join(c.dir, "network-pending.json"), p)
				if e == nil {
					c.networkPending = p
					_, e = c.native("PUT", "/mcb/ip", n, true)
				}
			}
		}
	case "network-confirm":
		if c.networkPending != nil {
			e = os.Remove(filepath.Join(c.dir, "network-pending.json"))
			if e == nil {
				c.networkPending = nil
			}
		}
	case "wifi":
		var v map[string]json.RawMessage
		e = json.Unmarshal(body, &v)
		if len(v) != 1 {
			e = errors.New("one Wi-Fi operation at a time")
		}
		for k := range v {
			if k != "enable" && k != "connect" && k != "disconnect" && k != "forget" && k != "scan" {
				e = errors.New("unknown Wi-Fi operation")
			}
		}
		if e == nil {
			_, e = c.native("PUT", "/mcb/wifisetting", body, true)
		}
	case "pool-add", "pool-delete":
		var p struct {
			URL  string `json:"url"`
			User string `json:"user"`
			Pass string `json:"pass"`
		}
		e = json.Unmarshal(body, &p)
		if p.URL == "" || p.User == "" || len(p.URL) > 512 || len(p.User) > 256 {
			e = errors.New("pool URL and worker are required")
		}
		if e == nil {
			endpoint := "/mcb/newpool"
			if path == "pool-delete" {
				endpoint = "/mcb/delpool"
			}
			_, e = c.native("PUT", endpoint, body, true)
		}
	case "pools":
		var pools []map[string]any
		e = json.Unmarshal(body, &pools)
		if len(pools) < 1 || len(pools) > 10 {
			e = errors.New("keep between 1 and 10 pools")
		}
		if e == nil {
			for i := range pools {
				pools[i]["pool-priority"] = i
			}
			_, e = c.native("PUT", "/mcb/pools", pools, true)
		}
	case "cloud", "rgb":
		endpoint := "/mcb/cmossetting"
		if path == "rgb" {
			endpoint = "/mcb/rgbsetting"
		}
		_, e = c.native("PUT", endpoint, body, true)
	case "identify":
		var q struct {
			Enabled bool `json:"enabled"`
		}
		e = json.Unmarshal(body, &q)
		if e == nil {
			var b []byte
			b, e = c.native("GET", "/mcb/setting", nil, false)
			var v map[string]any
			if e == nil {
				e = json.Unmarshal(b, &v)
			}
			if e == nil {
				v["ledcontrol"] = q.Enabled
				v["tempcontrol"] = true
				_, e = c.native("PUT", "/mcb/setting", v, true)
			}
		}
	case "password":
		var q struct {
			Old     string `json:"old"`
			New     string `json:"new"`
			Confirm string `json:"confirm"`
		}
		e = json.Unmarshal(body, &q)
		if q.New == "" || q.New != q.Confirm || len(q.New) > 128 {
			e = errors.New("new passwords must match (1–128 characters)")
		}
		if e == nil {
			_, e = c.login(q.Old)
		}
		if e == nil {
			old := c.cfg
			next := Config{q.New, q.Old}
			e = atomicJSON(filepath.Join(c.dir, "config.json"), next)
			if e == nil {
				query := url.Values{"oldpassword": {q.Old}, "newpassword": {q.New}, "cfmpassword": {q.Confirm}}
				_, e = c.native("PUT", "/user/updatepd?"+query.Encode(), nil, true)
				if e == nil {
					_, e = c.login(q.New)
				}
				if e == nil {
					c.cfg = Config{AdminPassword: q.New}
					e = atomicJSON(filepath.Join(c.dir, "config.json"), c.cfg)
					c.sessions = map[string]Session{}
				} else {
					_ = atomicJSON(filepath.Join(c.dir, "config.json"), old)
				}
			}
		}
	case "reboot":
		var q struct {
			Confirm string `json:"confirm"`
		}
		_ = json.Unmarshal(body, &q)
		if q.Confirm != "REBOOT" {
			e = errors.New("type REBOOT to confirm")
		} else {
			_, e = c.native("PUT", "/mcb/restart", map[string]any{}, true)
		}
	case "logout":
		if cookie, e := r.Cookie("paperclip"); e == nil {
			delete(c.sessions, cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: "paperclip", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	}
	if e != nil {
		jsonReply(w, 400, map[string]string{"error": e.Error()})
	} else {
		jsonReply(w, 200, map[string]bool{"ok": true})
	}
	return true
}
