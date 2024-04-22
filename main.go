package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	version = "0.2.0"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}

	var repository string
	switch os.Args[1] {
	case "-h", "--help", "help":
		usage()
		return
	case "-v", "--version", "version":
		if commit != "none" {
			fmt.Printf("gclone version %s, commit %s, built at %s\n", version, commit, date)
		} else {
			fmt.Printf("gclone version %s\n", version)
		}
		return
	default:
		repository = os.Args[1]
	}

	if _, err := exec.LookPath("git"); err != nil {
		fatal("git not found")
	}

	projectDir := getProjectDir(repository)

	if ok := isNotEmpty(projectDir); ok {
		fmt.Println(projectDir)
		return
	}

	if err := os.MkdirAll(filepath.Dir(projectDir), 0750); err != nil {
		fatal("failed create directory: %s", err)
	}

	cmd := exec.Command("git", "clone", repository)
	cmd.Dir = filepath.Dir(projectDir)
	if err := cmd.Run(); err != nil {
		fatal("failed clone repository: %s", err)
	}

	fmt.Println(projectDir)
}

// parse parses the given repository string and returns the parsed repository URL.
//
// It takes a string parameter named repository, which represents the repository to be parsed.
// The function returns a string that represents the parsed repository URL.
func parse(repository string) string {
	r := regexp.MustCompile(`^[^@\:\/]+@([^:]+):(.+)`)
	if r.MatchString(repository) {
		repository = "git://" + strings.ReplaceAll(repository, ":", "/")
	}

	repositoryURL, err := url.Parse(repository)
	if err != nil {
		fatal("failed parse repository: %w", err)
	}

	return repositoryURL.Hostname() + strings.TrimSuffix(strings.TrimSuffix(repositoryURL.Path, ".git"), "/")
}

// getProjectDir return directory from GIT_PROJECT_DIR variable and
// repository directory
func getProjectDir(repository string) string {
	gitProjectDir := os.Getenv("GIT_PROJECT_DIR")

	if strings.HasPrefix(gitProjectDir, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			fatal("%s", err)
		}
		gitProjectDir = strings.TrimPrefix(gitProjectDir, "~")
		gitProjectDir = filepath.Join(homeDir, gitProjectDir)
	}

	return filepath.Join(
		gitProjectDir,
		parse(repository),
	)
}

// isNotEmpty return true if directory exist and not empty
func isNotEmpty(name string) bool {
	f, err := os.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()

	_, err = f.Readdirnames(1)

	return !errors.Is(err, io.EOF)
}

// fatal print to Stderr message and exit from program
func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}

// Usage prints the usage of the program.
func usage() {
	fmt.Println("usage: gclone [-h] [-v] [REPOSITORY]")
	fmt.Println()
	fmt.Println("positional arguments:")
	fmt.Println("  REPOSITORY       Repository URL")
	fmt.Println()
	fmt.Println("optional arguments:")
	fmt.Println("  -h, --help       Show this help message and exit")
	fmt.Println("  -v, --version    Show the version number and exit")
	fmt.Println()
	fmt.Println("environment variables:")
	fmt.Println("  GIT_PROJECT_DIR  Directory to clone repositories")
	fmt.Println()
	fmt.Println("example:")
	fmt.Println("  GIT_PROJECT_DIR=\"$HOME/src\" gclone https://github.com/user/repo")
}
