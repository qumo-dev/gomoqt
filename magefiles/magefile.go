//go:build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

// ======================================
// SETUP
// ======================================

type Setup mg.Namespace

func (Setup) All() error {
	fmt.Println("Setting up development environment...")
	// Install golangci-lint
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		fmt.Println("Installing golangci-lint...")
		if err := sh.RunV("go", "install", "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"); err != nil {
			return err
		}
	}

	// Install Deno
	if _, err := exec.LookPath("deno"); err != nil {
		fmt.Println("Installing Deno...")
		if err := sh.RunV("sh", "-c", "curl -fsSL https://deno.land/x/install/install.sh | sh"); err != nil {
			return err
		}
	}

	return nil
}

func (Setup) Go() error {
	fmt.Println("Setting up Go environment...")

	// Check Go version
	fmt.Println("Checking Go version... (go version)")
	if err := goVersion(); err != nil {
		return err
	}

	// Install Go tools
	// Install golangci-lint
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		fmt.Println("Installing golangci-lint...")
		if err := sh.RunV("go", "install", "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"); err != nil {
			return err
		}
	}

	fmt.Println("Go environment setup complete.")

	return nil
}

func goVersion() error {
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return err
	}
	version := string(out)
	remote := struct {
		major int
		minor int
	}{
		major: 1,
		minor: 25,
	}

	required := struct {
		major int
		minor int
	}{
		major: 1,
		minor: 22,
	}

	re := regexp.MustCompile(`go version go([0-9]+)\.([0-9]+)`)
	matches := re.FindStringSubmatch(version)
	if len(matches) > 2 {
		major, _ := strconv.Atoi(matches[1])
		minor, _ := strconv.Atoi(matches[2])

		fmt.Printf("go version: %d.%d (local) | %d.%d (repository)", major, minor, remote.major, remote.minor)
		if major < required.major || (major == required.major && minor < required.minor) {
			fmt.Printf("   └─ >= %d.%d required", required.major, required.minor)
		}
	} else {
		return fmt.Errorf("failed to parse Go version from: %s", version)
	}

	return nil
}

func (Setup) Deno() error {
	fmt.Println("Setting up Deno environment...")

	// Check Deno version
	fmt.Println("Checking Deno version... (deno --version)")
	if err := denoVersion(); err != nil {
		return err
	}

	// Install Deno
	if _, err := exec.LookPath("deno"); err != nil {
		fmt.Println("Installing Deno...")
		if err := sh.RunV("sh", "-c", "curl -fsSL https://deno.land/x/install/install.sh | sh"); err != nil {
			return err
		}
	}

	return nil
}

func denoVersion() error {
	out, err := exec.Command("deno", "--version").Output()
	if err != nil {
		return err
	}
	output := string(out)
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Extract versions using regex
	re := regexp.MustCompile(`^(deno|v8|typescript)\s+([^\s]+)`)
	versions := map[string]string{}

	for _, line := range lines {
		if match := re.FindStringSubmatch(line); match != nil {
			versions[match[1]] = match[2]
		}
	}
	remote := struct {
		major int
		minor int
	}{
		major: 2,
		minor: 5,
	}

	// Format and output versions
	fmt.Println(
		"deno: %s, v8: %s, typescript: %s (local) | %d.%d (repository)",
		versions["deno"],
		versions["v8"],
		versions["typescript"],
		remote.major,
		remote.minor,
	)

	required := struct {
		major int
		minor int
	}{
		major: 2,
		minor: 0,
	}

	major, _ := strconv.Atoi(strings.Split(versions["deno"], ".")[0])
	minor, _ := strconv.Atoi(strings.Split(versions["deno"], ".")[1])
	if major < required.major || (major == required.major && minor < required.minor) {
		fmt.Printf("   └─ >= %d.%d required", required.major, required.minor)
	}
	return nil
}

// ======================================
// TESTING
// ======================================

type Test mg.Namespace

// Test runs all tests in the project
func (t Test) All() error {
	fmt.Println("Running tests...")
	return sh.RunV("go", "test", "./...")
}

// Coverage runs tests with coverage reporting
func (t Test) Coverage() error {
	fmt.Println("Running tests with coverage...")
	return sh.RunV("deno", "test", "--coverage=coverage")
}

// ======================================
// INTEROP
// ======================================

type Interop mg.Namespace

// Server runs the interop server
// Usage:
//
//	mage interop:server              - Run server (normal mode)
//	MKCERT=1 mage interop:server     - Run server with certificate regeneration
func (Interop) Server() error {
	fmt.Println("Starting interop server...")

	// Check for MKCERT environment variable
	mkcert := os.Getenv("MKCERT") != ""

	// If MKCERT env var is set, regenerate certificates first
	if mkcert {
		fmt.Println("Regenerating certificates...")
		if err := regenerateCerts(); err != nil {
			return err
		}
	}

	// Save current working directory
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	// Restore working directory when done
	defer func() {
		_ = os.Chdir(wd)
	}()

	// Change to interop server directory
	if err := os.Chdir("cmd/interop/server"); err != nil {
		return err
	}

	return sh.RunV("go", "run", ".")
}

// Client runs the interop client with the specified language.
// Usage:
//
//	mage interop:client go        - Run Go client
//	mage interop:client ts        - Run TypeScript client
func (Interop) Client(lang string) error {
	fmt.Printf("Starting interop test with %s client...\n", lang)

	// Save current working directory
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	// Change to interop directory and run main.go
	if err := os.Chdir("cmd/interop"); err != nil {
		return err
	}

	// Allow overriding server address via INTEROP_ADDR env var (e.g. INTEROP_ADDR=localhost:9001)
	addr := os.Getenv("INTEROP_ADDR")
	args := []string{"run", ".", "-lang", lang}
	if addr != "" {
		args = append(args, "-addr", addr)
	}
	return sh.RunV("go", args...)
}

// Default runs the interop client with Go (same as Client go)
func (i Interop) Default() error {
	return i.Client("go")
}

// ======================================
// DEVELOPMENT UTILITIES
// ======================================

// Lint runs the linter (golangci-lint)
func Lint() error {
	fmt.Println("Running linter...")
	// Check if golangci-lint is available
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		return fmt.Errorf("golangci-lint not found. Please install it first:\n  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest")
	}
	return sh.RunV("golangci-lint", "run")
}

// Fmt formats Go source code
func Fmt() error {
	fmt.Println("Formatting go code...")
	if err := sh.RunV("go", "fmt", "./..."); err != nil {
		return err
	}

	fmt.Println("Formatting TypeScript code...")
	if err := sh.RunV("deno", "fmt", "--ignore=**/*.md,**/*.yml,**/*.yaml"); err != nil {
		return err
	}
	return nil
}

// Check runs quality checks (formatting and linting)
func Check() error {
	mg.Deps(Fmt, Lint)
	fmt.Println("Quality checks complete.")
	return nil
}

// Build builds the project
func Build() error {
	fmt.Println("Building project...")
	return sh.RunV("go", "build", "./...")
}

// Clean removes generated files
func Clean() error {
	fmt.Println("Cleaning up generated files...")
	// Remove binaries directory if it exists
	if err := sh.Rm("./bin"); err != nil {
		// Ignore errors if directory doesn't exist
		if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Help displays available commands (default target)
func Help() {
	fmt.Println("Available Mage commands:")
	fmt.Println("")
	fmt.Println("Setup:")
	fmt.Println("  mage setup:all   - Setup all development tools")
	fmt.Println("  mage setup:go    - Setup Go environment")
	fmt.Println("  mage setup:deno  - Setup Deno environment")
	fmt.Println("")
	fmt.Println("Testing:")
	fmt.Println("  mage test:all      - Run all tests")
	fmt.Println("  mage test:coverage - Run tests with coverage")
	fmt.Println("")
	fmt.Println("Interop:")
	fmt.Println("  mage interop:server                    - Start interop server")
	fmt.Println("  MKCERT=1 mage interop:server           - Start server with certificate regeneration")
	fmt.Println("  mage interop:client go                 - Start server + Go client")
	fmt.Println("  mage interop:client ts                 - Start server + TypeScript client")
	fmt.Println("")
	fmt.Println("Development:")
	fmt.Println("  mage lint   - Run golangci-lint")
	fmt.Println("  mage fmt    - Format code")
	fmt.Println("  mage check  - Run quality checks (fmt and lint)")
	fmt.Println("  mage build  - Build project")
	fmt.Println("  mage clean  - Clean generated files")
	fmt.Println("  mage help   - Show this help message")
	fmt.Println("")
	fmt.Println("You can also run 'mage -l' to list all available targets.")
}

// regenerateCerts regenerates the server certificates
func regenerateCerts() error {
	// Find project root by looking for go.mod file
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Change to server directory
	serverDir := filepath.Join(wd, "cmd", "interop", "server")
	if err := os.Chdir(serverDir); err != nil {
		return err
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	// Remove old certificates if they exist
	_ = os.Remove("localhost.pem")
	_ = os.Remove("localhost-key.pem")

	// Generate new certificates
	fmt.Print("Generating new certificates with mkcert...")
	cmd := exec.Command("mkcert", "-cert-file", "localhost.pem", "-key-file", "localhost-key.pem", "localhost", "127.0.0.1", "::1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("...failed\n  Error: %v\n", err)
		return err
	}
	fmt.Println("...ok")
	return nil
}

// Default target - displays help when no target is specified
var Default = Help
