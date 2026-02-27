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
	fmt.Printf(
		"deno: %s, v8: %s, typescript: %s (local) | %d.%d (repository)\n",
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

// Client runs the interop client with the specified language.

// Ts runs the interop server+client inside a Docker container using the
// TypeScript client.  This replaces the old bare-metal interop:client ts.
func (Interop) Ts() error {
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Println("docker not found; please install Docker to use this target")
		return err
	}
	fmt.Println("Building interop docker image...")
	if err := sh.RunV("docker", "build", "-t", "gomoqt-interop", "."); err != nil {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	fmt.Println("Running TypeScript interop test inside container...")
	return sh.RunV("docker", "run", "--rm",
		"-v", fmt.Sprintf("%s:/work", wd),
		"-w", "/work",
		"-e", "MKCERT=1",
		"gomoqt-interop",
		"go", "run", "./cmd/interop", "-lang", "ts")
}

// Go runs the interop server+client inside a Docker container using the
// Go client.  This replaces the old bare-metal interop:client go.
func (Interop) Go() error {
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Println("docker not found; please install Docker to use this target")
		return err
	}
	fmt.Println("Building interop docker image...")
	if err := sh.RunV("docker", "build", "-t", "gomoqt-interop", "."); err != nil {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	fmt.Println("Running Go interop test inside container...")
	return sh.RunV("docker", "run", "--rm",
		"-v", fmt.Sprintf("%s:/work", wd),
		"-w", "/work",
		"-e", "MKCERT=1",
		"gomoqt-interop",
		"go", "run", "./cmd/interop", "-lang", "go")
}

// Default now runs the TypeScript interop test inside Docker.
func (i Interop) Default() error {
	fmt.Println("Default interop target now uses Docker TS client; executing mage interop ts")
	return i.Ts()
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
	fmt.Println("  mage interop ts                       - Dockerized interop using TypeScript client (preferred)")
	fmt.Println("  mage interop go                       - Dockerized interop using Go client")
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
