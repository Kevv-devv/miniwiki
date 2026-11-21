package main

import "testing"

func TestDisplayListenURL(t *testing.T) {
	tests := []struct {
		name string
		addr string
		tls  bool
		want string
	}{
		{"default wildcard", ":8080", false, "http://localhost:8080"},
		{"explicit IPv4", "127.0.0.1:9090", false, "http://127.0.0.1:9090"},
		{"all IPv4", "0.0.0.0:8080", false, "http://localhost:8080"},
		{"explicit IPv6", "[::1]:9090", false, "http://[::1]:9090"},
		{"all IPv6", "[::]:8080", false, "http://localhost:8080"},
		{"hostname", "wiki.example.com:8443", true, "https://wiki.example.com:8443"},
		{"broken address still reported", "invalid", false, "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := displayListenURL(tt.addr, tt.tls); got != tt.want {
				t.Errorf("displayListenURL(%q, %t) = %q; want %q", tt.addr, tt.tls, got, tt.want)
			}
		})
	}
}
