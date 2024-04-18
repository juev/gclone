package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	httpPrefix = `https://`
	gitPrefix  = `git@`
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	_, err := exec.LookPath("git")
	if err != nil {
		fatal("git not found")
	}

	repository := os.Args[1]
	if !strings.HasPrefix(repository, httpPrefix) &&
		!strings.HasPrefix(repository, gitPrefix) {
		repository = "https://" + repository
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

// parseRepositoryURL get directory from repository URL
// URL can be http and ssh
func parseRepositoryURL(repository string) (dir string) {
	dir = strings.TrimPrefix(repository, httpPrefix)
	dir = strings.TrimPrefix(dir, gitPrefix)
	dir = strings.TrimSuffix(dir, ".git")
	dir = strings.ReplaceAll(dir, "~", "")
	dir = strings.Replace(dir, ":", string(os.PathSeparator), 1)

	if dir == "" {
		usage()
	}

	return dir
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
		filepath.Clean(gitProjectDir),
		filepath.Clean(parseRepositoryURL(repository)),
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
	os.Exit(0)
}

// usage print program usage
func usage() {
	fmt.Printf(
		`example: 
GIT_PROJECT_DIR="~/src" gclone https://github.com/juev/gclone.git

the repository must be in one of the following formats:

- https://github.com/repository/name
- git@github.com/repository/name
`,
	)
	os.Exit(0)
}
