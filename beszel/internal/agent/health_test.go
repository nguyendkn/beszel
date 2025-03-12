//go:build testing
// +build testing

package agent_test

import (
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"beszel/internal/agent"
)

// setupTestServer creates a temporary server for testing
func setupTestServer(t *testing.T) (string, func()) {
	// Create a temporary socket file for Unix socket testing
	tempSockFile := os.TempDir() + "/beszel_health_test.sock"

	// Clean up any existing socket file
	os.Remove(tempSockFile)

	// Create a listener
	listener, err := net.Listen("unix", tempSockFile)
	require.NoError(t, err, "Failed to create test listener")

	// Start a simple server in a goroutine
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return // Listener closed
		}
		defer conn.Close()
		// Just accept the connection and do nothing
	}()

	// Return the socket file path and a cleanup function
	return tempSockFile, func() {
		listener.Close()
		os.Remove(tempSockFile)
	}
}

// setupTCPTestServer creates a temporary TCP server for testing
func setupTCPTestServer(t *testing.T) (string, func()) {
	// Listen on a random available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "Failed to create test listener")

	// Get the port that was assigned
	addr := listener.Addr().(*net.TCPAddr)
	port := addr.Port

	// Start a simple server in a goroutine
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return // Listener closed
		}
		defer conn.Close()
		// Just accept the connection and do nothing
	}()

	// Return the port and a cleanup function
	return fmt.Sprintf("%d", port), func() {
		listener.Close()
	}
}

// checkDefaultPortAvailable checks if the default port is available
func checkDefaultPortAvailable() bool {
	// Try to listen on the default port
	listener, err := net.Listen("tcp", ":45876")
	if err != nil {
		// Port is in use
		return false
	}
	defer listener.Close()
	return true
}

func TestHealth(t *testing.T) {
	t.Run("server is running (unix socket)", func(t *testing.T) {
		// Setup a test server
		sockFile, cleanup := setupTestServer(t)
		defer cleanup()

		// Set environment variables to point to our test server
		os.Setenv("BESZEL_AGENT_NETWORK", "unix")
		os.Setenv("BESZEL_AGENT_LISTEN", sockFile)
		defer func() {
			os.Unsetenv("BESZEL_AGENT_NETWORK")
			os.Unsetenv("BESZEL_AGENT_LISTEN")
		}()

		// Run the health check
		result := agent.Health()

		// Verify the result
		assert.Equal(t, 0, result, "Health check should return 0 when server is running")
	})

	t.Run("server is running (tcp port)", func(t *testing.T) {
		// Setup a test server
		port, cleanup := setupTCPTestServer(t)
		defer cleanup()

		// Set environment variables to point to our test server
		os.Setenv("BESZEL_AGENT_NETWORK", "tcp")
		os.Setenv("BESZEL_AGENT_LISTEN", ":"+port)
		defer func() {
			os.Unsetenv("BESZEL_AGENT_NETWORK")
			os.Unsetenv("BESZEL_AGENT_LISTEN")
		}()

		// Run the health check
		result := agent.Health()

		// Verify the result
		assert.Equal(t, 0, result, "Health check should return 0 when server is running")
	})

	t.Run("server is not running", func(t *testing.T) {
		// Set environment variables to point to a non-existent server
		os.Setenv("BESZEL_AGENT_NETWORK", "tcp")
		os.Setenv("BESZEL_AGENT_LISTEN", "127.0.0.1:65535") // Using a port that's likely not in use
		defer func() {
			os.Unsetenv("BESZEL_AGENT_NETWORK")
			os.Unsetenv("BESZEL_AGENT_LISTEN")
		}()

		// Run the health check
		result := agent.Health()

		// Verify the result
		assert.Equal(t, 1, result, "Health check should return 1 when server is not running")
	})

	t.Run("default address", func(t *testing.T) {
		// Clear environment variables to test default behavior
		os.Unsetenv("BESZEL_AGENT_NETWORK")
		os.Unsetenv("BESZEL_AGENT_LISTEN")
		os.Unsetenv("NETWORK")
		os.Unsetenv("LISTEN")
		os.Unsetenv("PORT")

		// Check if the default port is available
		portAvailable := checkDefaultPortAvailable()

		// Run the health check
		result := agent.Health()

		if portAvailable {
			// If the port is available, the health check should fail
			assert.Equal(t, 1, result, "Health check should return 1 when using default address and server is not running")
		} else {
			// If the port is in use, the health check might succeed
			t.Logf("Default port 45876 is in use, health check might succeed")
			// We don't assert anything here since we can't control what's running on the port
		}
	})

	t.Run("legacy PORT environment variable", func(t *testing.T) {
		// Setup a test server
		port, cleanup := setupTCPTestServer(t)
		defer cleanup()

		// Set legacy PORT environment variable
		os.Unsetenv("BESZEL_AGENT_NETWORK")
		os.Unsetenv("BESZEL_AGENT_LISTEN")
		os.Unsetenv("NETWORK")
		os.Unsetenv("LISTEN")
		os.Setenv("BESZEL_AGENT_PORT", port)
		defer func() {
			os.Unsetenv("BESZEL_AGENT_PORT")
		}()

		// Run the health check
		result := agent.Health()

		// Verify the result
		assert.Equal(t, 0, result, "Health check should return 0 when server is running on legacy PORT")
	})
}
