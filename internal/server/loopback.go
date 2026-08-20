package server

import (
	"fmt"
	"net"
)

// CheckLoopbackAddr accepts only loopback host:port (127.0.0.0/8, ::1,
// localhost). An empty host means every interface and is refused.
func CheckLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("address %q must be loopback host:port, e.g. 127.0.0.1:4120", addr)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("address %q must be loopback, e.g. 127.0.0.1:4120", addr)
}
