//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type hidDriver struct {
	f         *os.File
	dualsense bool
	path      string
}

func (d *hidDriver) Name() string {
	if d.dualsense {
		return "dualsense"
	}
	return "ds4"
}

func (d *hidDriver) Close() error { return d.f.Close() }

// Set writes a USB output report. Bluetooth reports need a CRC and are not
// handled here; use a USB cable for now.
func (d *hidDriver) Set(low, high float64) error {
	report := make([]byte, 64)
	if d.dualsense {
		report[0] = 0x02
		report[1] = 0xff
		report[2] = byte(clamp(high) * 255)
		report[3] = byte(clamp(low) * 255)
	} else {
		report[0] = 0x05
		report[1] = 0xff
		report[4] = byte(clamp(high) * 255)
		report[5] = byte(clamp(low) * 255)
	}
	_, err := d.f.Write(report)
	return err
}

func openHID() Driver {
	files, _ := filepath.Glob("/dev/hidraw*")
	for _, p := range files {
		name := strings.ToLower(hidName(p))
		if strings.Contains(name, "sony") || strings.Contains(name, "dualsense") || strings.Contains(name, "wireless controller") {
			f, err := os.OpenFile(p, os.O_RDWR, 0)
			if err != nil {
				continue
			}
			return &hidDriver{f: f, dualsense: strings.Contains(name, "dualsense"), path: p}
		}
	}
	return nil
}

func hidName(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 1024)
	// HIDIOCGRAWNAME(1024)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(0x84004806), uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return ""
	}
	i := 0
	for i < len(buf) && buf[i] != 0 {
		i++
	}
	return string(buf[:i])
}
