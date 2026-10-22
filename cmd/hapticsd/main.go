// hapticsd is a small local daemon that turns normalised low/high amplitudes
// from a browser player into controller rumble. It speaks JSON over WebSocket
// and keeps a single writer per device.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"math"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Driver interface {
	Set(low, high float64) error
	Name() string
	Close() error
}

type mockDriver struct {
	lastLow, lastHigh float64
	lastLog           time.Time
	mu                sync.Mutex
}

func (m *mockDriver) Name() string { return "mock" }
func (m *mockDriver) Close() error { return nil }
func (m *mockDriver) Set(low, high float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if math.Abs(low-m.lastLow) < 0.05 && math.Abs(high-m.lastHigh) < 0.05 {
		return nil
	}
	if now.Sub(m.lastLog) < 250*time.Millisecond {
		return nil
	}
	m.lastLow, m.lastHigh, m.lastLog = low, high, now
	log.Printf("mock low=%.2f high=%.2f", low, high)
	return nil
}

type engine struct {
	mu                     sync.Mutex
	targetLow, targetHigh  float64
	intensity              float64
	curLow, curHigh        float64
}

func (e *engine) tick(low, high, intensity float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if intensity < 0 {
		intensity = 0
	}
	if intensity > 1 {
		intensity = 1
	}
	e.intensity = intensity
	e.targetLow = clamp(low) * intensity
	e.targetHigh = clamp(high) * intensity
}

func (e *engine) stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.targetLow, e.targetHigh = 0, 0
}

// step moves the current amplitude toward the target and returns the values.
func (e *engine) step() (float64, float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	k := 0.35 // attack
	if e.targetLow < e.curLow || e.targetHigh < e.curHigh {
		k = 0.15 // release
	}
	e.curLow += (e.targetLow - e.curLow) * k
	e.curHigh += (e.targetHigh - e.curHigh) * k
	if e.curLow < 0.005 {
		e.curLow = 0
	}
	if e.curHigh < 0.005 {
		e.curHigh = 0
	}
	return e.curLow, e.curHigh
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8771", "listen address")
	driverName := flag.String("driver", "auto", "auto | mock | hidraw")
	flag.Parse()

	var drv Driver
	switch *driverName {
	case "mock":
		drv = &mockDriver{}
	case "hidraw":
		drv = openHID()
		if drv == nil {
			log.Fatal("no supported controller on /dev/hidraw*")
		}
	default:
		if d := openHID(); d != nil {
			drv = d
		} else {
			log.Print("no controller found; using mock driver")
			drv = &mockDriver{}
		}
	}
	defer drv.Close()
	log.Printf("hapticsd driver=%s addr=%s", drv.Name(), *addr)

	eng := &engine{}
	go func() {
		t := time.NewTicker(30 * time.Millisecond)
		lastLow, lastHigh := 0.0, 0.0
		for range t.C {
			low, high := eng.step()
			if math.Abs(low-lastLow) > 0.01 || math.Abs(high-lastHigh) > 0.01 {
				_ = drv.Set(low, high)
				lastLow, lastHigh = low, high
			}
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		low, high := eng.step()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "driver": drv.Name(), "low": low, "high": high})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(32 << 10)
		for {
			_, data, err := c.Read(context.Background())
			if err != nil {
				return
			}
			var msg struct {
				Op        string  `json:"op"`
				Low       float64 `json:"low"`
				High      float64 `json:"high"`
				Intensity float64 `json:"intensity"`
			}
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			switch msg.Op {
			case "tick":
				eng.tick(msg.Low, msg.High, msg.Intensity)
			case "stop":
				eng.stop()
			case "hello":
				// no-op; the first tick carries state
			}
		}
	})

	log.Printf("listening on http://%s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
