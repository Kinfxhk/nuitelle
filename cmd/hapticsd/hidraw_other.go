//go:build !linux

package main

// openHID is only implemented on Linux for now. On other platforms the daemon
// falls back to the mock driver.
func openHID() Driver { return nil }
