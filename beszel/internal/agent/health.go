package agent

import (
	"log/slog"
	"net"
	"time"
)

// Health checks if the agent's server is running by attempting to connect to it.
// It returns exit code 0 if the server is running, 1 otherwise.
// This is designed to be used as a Docker health check.
func Health() int {
	addr := GetAddress("")
	network := GetNetwork(addr)
	conn, err := net.DialTimeout(network, addr, 2*time.Second)
	if err != nil {
		slog.Error("Health check failed", "error", err)
		return 1
	}
	defer conn.Close()
	return 0
}
