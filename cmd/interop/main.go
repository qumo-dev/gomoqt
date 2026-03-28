package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func findRootDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", os.ErrNotExist
}

func main() {
	addr := flag.String("addr", "localhost:9000", "server address")
	lang := flag.String("lang", "go", "client language: go or ts")
	flag.Parse()

	slog.Info(" === MOQ Interop Test ===")

	// Create context for server lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background (run server package under cmd/interop)
	// Server binds to :port (all interfaces), not hostname:port
	clientURL := *addr
	if !strings.Contains(clientURL, "://") {
		if *lang == "go" {
			clientURL = "moqt://" + clientURL
		} else {
			clientURL = "https://" + clientURL
		}
	}

	parsedAddr, err := url.Parse(clientURL)
	if err != nil {
		slog.Error("invalid interop addr", "addr", *addr, "error", err)
		return
	}

	port := parsedAddr.Port()
	if port == "" {
		slog.Error("interop addr must include explicit port", "addr", *addr)
		return
	}
	// determine project root so we can run the server package regardless of cwd
	root, err := findRootDir()
	if err != nil {
		// fall back to relative path; it will likely fail below
		root = ""
	}
	serverPath := filepath.Join(root, "cmd", "interop", "server")
	serverCmd := exec.CommandContext(ctx, "go", "run", serverPath, "-addr", ":"+port)

	// Pipe server output so we can reformat and unify logs
	serverStdout, err := serverCmd.StdoutPipe()
	if err != nil {
		slog.Error("Failed to capture server stdout: " + err.Error())
		return
	}
	serverStderr, err := serverCmd.StderrPipe()
	if err != nil {
		slog.Error("Failed to capture server stderr: " + err.Error())
		return
	}

	err = serverCmd.Start()
	if err != nil {
		slog.Error("Failed to start server: " + err.Error())
		return
	}

	// Ensure server is killed when we exit
	// Stream server output in background
	var wg sync.WaitGroup
	// Channels to detect when the child server or client declare completion
	// serverReady := make(chan struct{}, 1)
	wg.Go(func() {
		streamAndLog("Server", serverStdout)
	})
	wg.Go(func() {
		streamAndLog("Server", serverStderr)
	})

	defer func() {
		if serverCmd.Process != nil {
			slog.Debug(" Killing server process...")
			_ = serverCmd.Process.Kill()
			_ = serverCmd.Wait()
			slog.Debug(" Server process terminated")
		}
	}()

	// Wait for server to start listening by polling the port.  The server
	// process is "go run" which may take some time to build inside Docker,
	// so a fixed sleep is unreliable.
	{
		addrToCheck := "localhost:" + port
		for range 40 { // try up to 20 seconds
			c, err := net.Dial("tcp", addrToCheck)
			if err == nil {
				c.Close()
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Run client and wait for completion
	slog.Debug(" Starting client...")
	var clientCmd *exec.Cmd
	var errClient error
	if *lang == "ts" {
		clientCmd, errClient = buildTSClientCmd(ctx, *addr)
		if errClient != nil {
			slog.Error("failed to prepare TypeScript client", "error", errClient)
			return
		}
	} else {
		// Go client
		clientPath := filepath.Join(root, "cmd", "interop", "client")
		clientCmd = exec.CommandContext(ctx, "go", "run", clientPath, "-addr", clientURL)
	}

	clientStdout, err := clientCmd.StdoutPipe()
	if err != nil {
		slog.Error("Failed to capture client stdout: " + err.Error())
		return
	}
	clientStderr, err := clientCmd.StderrPipe()
	if err != nil {
		slog.Error("Failed to capture client stderr: " + err.Error())
		return
	}

	if err = clientCmd.Start(); err != nil {
		slog.Error(" Failed to start client: " + err.Error())
		return
	}

	wg.Go(func() {
		streamAndLog("Client", clientStdout)
	})
	wg.Go(func() {
		streamAndLog("Client", clientStderr)
	})

	// Wait for the client process to finish running and let the server
	// continue until it declares its operation complete as well. Use a
	// timeout to avoid waiting forever in case things fail.
	clientErr := clientCmd.Wait()

	// Stop the server to unblock output streams
	cancel()

	// Kill server process immediately
	if serverCmd.Process != nil {
		_ = serverCmd.Process.Kill()
		_ = serverCmd.Wait()
	}

	// Wait for all streaming goroutines to finish
	wg.Wait()

	slog.Info(" === Interop Test Completed ===")

	if clientErr != nil {
		slog.Error("Client failed: " + clientErr.Error())
		os.Exit(1)
	}
}

// buildTSClientCmd builds the *exec.Cmd to run the TypeScript interop client.
// It locates the project root, computes the certificate hash for the server
// cert (used for pinning), and constructs the command invocation with the
// proper working directory.
func buildTSClientCmd(ctx context.Context, addr string) (*exec.Cmd, error) {
	root, err := findRootDir()
	if err != nil {
		// fallback to cwd; most tests run from repository root so this should work
		root, _ = os.Getwd()
		slog.Warn("could not determine project root, falling back to cwd", "error", err)
	}

	// compute cert hash (best effort)
	hash := ""
	certFile := filepath.Join(root, "cmd", "interop", "server", "localhost.pem")
	if h, err := computeCertHash(certFile); err == nil {
		hash = h
		slog.Debug("computed cert hash", "hash", h)
	} else {
		slog.Warn("unable to compute cert hash", "error", err)
	}

	// figure out path to moq-web directory relative to project root
	moqWebDir := filepath.Join(root, "moq-web")
	if _, statErr := os.Stat(moqWebDir); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, fmt.Errorf("moq-web directory not found at %s", moqWebDir)
		}
		return nil, fmt.Errorf("failed to stat moq-web directory: %w", statErr)
	}

	args := []string{"run", "--unstable-net", "--allow-all",
		"cli/interop/run_secure.ts", "--addr", "https://" + addr}
	if hash != "" {
		args = append(args, "--insecure", "--cert-hash", hash)
	}

	cmd := exec.CommandContext(ctx, "deno", args...)
	cmd.Dir = moqWebDir
	return cmd, nil
}

// streamAndLog reads from reader line by line and logs each line prefixed with
// the source name. It also notifies channels when specific patterns are found.
func streamAndLog(source string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Printf("[%s] %s\n", source, line)
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("Error reading output stream", "error", err)
	}
}

// computeCertHash computes the SHA-256 hash of the first certificate in a PEM file
// and returns the result in base64. Used for pinning the interop server cert.
func computeCertHash(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("no certificate block found in %s", path)
	}

	h := sha256.Sum256(block.Bytes)
	return base64.StdEncoding.EncodeToString(h[:]), nil
}
