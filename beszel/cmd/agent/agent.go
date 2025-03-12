package main

import (
	"beszel"
	"beszel/internal/agent"
	"flag"
	"fmt"
	"log"
	"os"

	"golang.org/x/crypto/ssh"
)

// cli options
type cmdOptions struct {
	key    string // key is the public key(s) for SSH authentication.
	listen string // listen is the address or port to listen on.
}

// parseFlags parses the command line flags and populates the config struct.
func (opts *cmdOptions) parseFlags() {
	flag.StringVar(&opts.key, "key", "", "Public key(s) for SSH authentication")
	flag.StringVar(&opts.listen, "listen", "", "Address or port to listen on")

	flag.Usage = func() {
		fmt.Printf("Usage: %s [options] [subcommand]\n", os.Args[0])
		fmt.Println("\nOptions:")
		flag.PrintDefaults()
		fmt.Println("\nSubcommands:")
		fmt.Println("  version      Display the version")
		fmt.Println("  help         Display this help message")
		fmt.Println("  update       Update the agent to the latest version")
		fmt.Println("  health       Check if the agent is running (for Docker health checks)")
	}
}

// handleSubcommand handles subcommands such as version, help, and update.
// It returns true if a subcommand was handled, false otherwise.
func handleSubcommand() bool {
	if len(os.Args) <= 1 {
		return false
	}
	switch os.Args[1] {
	case "health":
		exitCode := agent.Health()
		os.Exit(exitCode)
	case "version", "-v":
		fmt.Println(beszel.AppName+"-agent", beszel.Version)
		os.Exit(0)
	case "help":
		flag.Usage()
		os.Exit(0)
	case "update":
		agent.Update()
		os.Exit(0)
	default:
		return false
	}
	return true
}

// loadPublicKeys loads the public keys from the command line flag, environment variable, or key file.
func (opts *cmdOptions) loadPublicKeys() ([]ssh.PublicKey, error) {
	// Try command line flag first
	if opts.key != "" {
		return agent.ParseKeys(opts.key)
	}

	// Try environment variable
	if key, ok := agent.GetEnv("KEY"); ok && key != "" {
		return agent.ParseKeys(key)
	}

	// Try key file
	keyFile, ok := agent.GetEnv("KEY_FILE")
	if !ok {
		return nil, fmt.Errorf("no key provided: must set -key flag, KEY env var, or KEY_FILE env var. Use 'beszel-agent help' for usage")
	}

	pubKey, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}
	return agent.ParseKeys(string(pubKey))
}

func (opts *cmdOptions) getAddress() string {
	return agent.GetAddress(opts.listen)
}

func main() {
	var opts cmdOptions
	opts.parseFlags()

	if handleSubcommand() {
		return
	}

	flag.Parse()

	var serverConfig agent.ServerOptions
	var err error
	serverConfig.Keys, err = opts.loadPublicKeys()
	if err != nil {
		log.Fatal("Failed to load public keys:", err)
	}

	addr := opts.getAddress()
	serverConfig.Addr = addr
	serverConfig.Network = agent.GetNetwork(addr)

	agent := agent.NewAgent()
	if err := agent.StartServer(serverConfig); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
