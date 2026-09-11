package brokerhttp

import (
	"net"
	"strings"
)

const DefaultListen = "127.0.0.1:8787"

func ParseListen(addr string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "", err
	}
	if host != "127.0.0.1" {
		return "", listenMustBeLoopback(addr)
	}
	if port == "" {
		return "", listenMustBeLoopback(addr)
	}
	return net.JoinHostPort(host, port), nil
}
