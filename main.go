package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/pflag"
)

var (
	version = "0.3.3"
	commit  = "none"
	date    = "unknown"
)

var r = regexp.MustCompile(`^(?:.*://)?(?:[^@]+@)?([^:/]+)(?::\d+)?(?:/|:)?(.*)$`)

func main() {
	var showCommandHelp, showVersionInfo, quiet bool
	pflag.BoolVarP(&showCommandHelp, "help", "h", false, "Show this help message and exit")
	pflag.BoolVarP(&showVersionInfo, "version", "v", false, "Show the version number and exit")
	pflag.BoolVarP(&quiet, "quiet", "q", false, "Suppress output")
	pflag.Parse()

	if showCommandHelp {
		usage()
		return
	}

	if showVersionInfo {
		if commit != "none" {
			fmt.Printf("gclone version %s, commit %s, built at %s\n", version, commit, date)
		} else {
			fmt.Printf("gclone version %s\n", version)
		}
		return
	}

	if _, err := exec.LookPath("git"); err != nil {
		prnt("git not found")
		os.Exit(1)
	}

	var projectDir string
	for _, arg := range pflag.Args() {
		repository := arg
		projectDir = getProjectDir(repository)

		if ok := isDirectoryNotEmpty(projectDir); ok {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(projectDir), 0750); err != nil {
			prnt("failed create directory: %s", err)
			continue
		}

		cmd := exec.Command("git", "clone", repository)
		if !quiet {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
		cmd.Dir = filepath.Dir(projectDir)
		if err := cmd.Run(); err != nil {
			prnt("failed clone repository: %s", err)
			continue
		}
		if !quiet {
			fmt.Println()
		}
	}

	// Print latest project directory
	fmt.Println(projectDir)
}

// normalize normalizes the given repository string and returns the parsed repository URL.
func normalize(repo string) string {
	match := r.FindStringSubmatch(repo)
	if len(match) != 3 {
		return ""
	}
	path := match[2]
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimPrefix(path, "~")
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")

	return filepath.Join(match[1], path)
}

// getProjectDir returns the project directory based on the given repository URL.
// It retrieves the GIT_PROJECT_DIR environment variable and normalizes it.
// If the GIT_PROJECT_DIR starts with "~", it replaces it with the user's home directory.
// The normalized repository URL is then joined with the GIT_PROJECT_DIR to form the project directory path.
// The project directory path is returned as a string.
func getProjectDir(repository string) string {
	gitProjectDir := os.Getenv("GIT_PROJECT_DIR")
	if strings.HasPrefix(gitProjectDir, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		gitProjectDir = filepath.Join(homeDir, gitProjectDir[1:])
	}

	return filepath.Join(gitProjectDir, normalize(repository))
}

// isDirectoryNotEmpty checks if the specified directory is not empty.
// It uses the Readdirnames function to get the directory contents without loading full FileInfo
// structures for each entry. If there are any entries, it returns true. Otherwise, it returns false.
func isDirectoryNotEmpty(name string) bool {
	f, err := os.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	return err == nil
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

func prnt(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	fmt.Fprintln(os.Stderr)
}
