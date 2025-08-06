package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/spf13/pflag"
)

var (
	version = "0.3.4"
	commit  = "none"
	date    = "unknown"
)

var regexPool = sync.Pool{
	New: func() any {
		return regexp.MustCompile(`^(?:.*://)?(?:[^@]+@)?([^:/]+)(?::\d+)?[/:]?(.*)$`)
	},
}

type cacheEntry struct {
	exists    bool
	timestamp time.Time
}

type DirCache struct {
	cache map[string]cacheEntry
	mutex sync.RWMutex
	ttl   time.Duration
}

var dirCache = &DirCache{
	cache: make(map[string]cacheEntry),
	ttl:   30 * time.Second,
}

// Security validation functions

var (
	// Allowed URL schemes for git repositories
	allowedSchemes = map[string]bool{
		"https": true,
		"http":  true,
		"ssh":   true,
		"git":   true,
	}
	
	// Dangerous characters that could be used for command injection
	dangerousChars = regexp.MustCompile(`[;&|$\x60<>(){}[\]!*?]`)
	
	// Valid hostname pattern - more restrictive than RFC but safer
	validHostname = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)*$`)
	
	// Valid path characters for Git repositories
	validRepoPath = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)
)

// validateRepositoryURL performs comprehensive security validation of repository URLs
func validateRepositoryURL(repo string) error {
	if repo == "" {
		return errors.New("repository URL cannot be empty")
	}

	// Check for dangerous characters that could indicate command injection
	if dangerousChars.MatchString(repo) {
		return errors.New("repository URL contains dangerous characters")
	}

	// Handle SSH URLs like git@github.com:user/repo.git
	if strings.Contains(repo, "@") && strings.Contains(repo, ":") && !strings.Contains(repo, "://") {
		return validateSSHURL(repo)
	}

	// Parse as regular URL
	parsedURL, err := url.Parse(repo)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// Validate scheme
	if parsedURL.Scheme != "" && !allowedSchemes[parsedURL.Scheme] {
		return fmt.Errorf("unsupported URL scheme: %s", parsedURL.Scheme)
	}

	// Validate hostname
	if parsedURL.Host != "" && !validHostname.MatchString(parsedURL.Host) {
		return fmt.Errorf("invalid hostname: %s", parsedURL.Host)
	}

	// Validate path for traversal attacks
	if err := validatePath(parsedURL.Path); err != nil {
		return err
	}

	return nil
}

// validateSSHURL validates SSH-style Git URLs (git@host:path)
func validateSSHURL(repo string) error {
	parts := strings.SplitN(repo, "@", 2)
	if len(parts) != 2 {
		return errors.New("invalid SSH URL format")
	}

	hostPath := parts[1]
	hostPathParts := strings.SplitN(hostPath, ":", 2)
	if len(hostPathParts) != 2 {
		return errors.New("invalid SSH URL format - missing colon separator")
	}

	host := hostPathParts[0]
	path := hostPathParts[1]

	// Validate hostname
	if !validHostname.MatchString(host) {
		return fmt.Errorf("invalid hostname in SSH URL: %s", host)
	}

	// Validate path
	if err := validatePath(path); err != nil {
		return err
	}

	return nil
}

// validatePath checks for path traversal attacks and invalid characters
func validatePath(path string) error {
	if path == "" {
		return nil // Empty path is acceptable
	}

	// Check for path traversal attempts
	if strings.Contains(path, "..") {
		return errors.New("path traversal detected in URL")
	}

	// Check for absolute paths that could escape intended directory
	if strings.HasPrefix(path, "/") {
		// Remove leading slash for validation but allow it
		path = strings.TrimPrefix(path, "/")
	}

	// Allow tilde for user directories but validate the rest
	if strings.HasPrefix(path, "~") {
		path = strings.TrimPrefix(path, "~")
		if strings.HasPrefix(path, "/") {
			path = strings.TrimPrefix(path, "/")
		}
	}

	// Validate remaining path characters (allow common Git repo path characters)
	if path != "" && !validRepoPath.MatchString(path) {
		return fmt.Errorf("invalid characters in repository path: %s", path)
	}

	return nil
}

// secureGitClone performs git clone with additional security measures including timeout
func secureGitClone(repository, targetDir string, quiet bool) error {
	// Double-check validation (defense in depth)
	if err := validateRepositoryURL(repository); err != nil {
		return fmt.Errorf("security validation failed: %w", err)
	}

	// Validate target directory to prevent directory traversal
	cleanTargetDir := filepath.Clean(targetDir)
	if strings.Contains(cleanTargetDir, "..") {
		return errors.New("target directory contains path traversal")
	}

	// Create context with timeout to prevent hanging operations
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Create command with explicit arguments and timeout context (no shell interpretation)
	cmd := exec.CommandContext(ctx, "git", "clone", "--", repository)
	
	// Set working directory
	cmd.Dir = cleanTargetDir
	
	// Configure output
	if !quiet {
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
	}

	// Execute with timeout protection
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("git clone operation timed out after 10 minutes: %s", repository)
		}
		return err
	}
	
	return nil
}

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

	args := pflag.Args()
	
	// Validate that we have arguments
	if len(args) == 0 {
		prnt("error: no repository URLs provided")
		usage()
		os.Exit(1)
	}

	// Validate each argument is not empty
	for i, arg := range args {
		if strings.TrimSpace(arg) == "" {
			prnt("error: argument %d is empty", i+1)
			os.Exit(1)
		}
	}

	// Check git availability with better error message
	if _, err := exec.LookPath("git"); err != nil {
		prnt("error: git command not found in PATH")
		prnt("please install git: https://git-scm.com/downloads")
		os.Exit(1)
	}

	var lastSuccessfulProjectDir string
	var processedCount int
	var failedCount int

	for _, arg := range args {
		repository := strings.TrimSpace(arg)
		processedCount++
		
		// Security: Validate repository URL before processing
		if err := validateRepositoryURL(repository); err != nil {
			prnt("invalid repository URL '%s': %s", repository, err)
			failedCount++
			continue
		}

		projectDir, err := getProjectDir(repository)
		if err != nil {
			prnt("failed to determine project directory for '%s': %s", repository, err)
			failedCount++
			continue
		}

		// Check if directory already exists and is not empty
		if ok := isDirectoryNotEmpty(projectDir); ok {
			if !quiet {
				prnt("repository already exists: %s", projectDir)
			}
			// Still consider this successful for output purposes
			lastSuccessfulProjectDir = projectDir
			continue
		}

		// Create parent directory
		if err := os.MkdirAll(filepath.Dir(projectDir), 0750); err != nil {
			prnt("failed create directory: %s", err)
			failedCount++
			continue
		}

		// Security: Use secure git clone with validated arguments
		if err := secureGitClone(repository, filepath.Dir(projectDir), quiet); err != nil {
			prnt("failed clone repository '%s': %s", repository, err)
			failedCount++
			continue
		}

		// Clone was successful
		lastSuccessfulProjectDir = projectDir
		if !quiet {
			fmt.Fprintln(os.Stderr)
		}
	}

	// Print summary if multiple repositories were processed
	if processedCount > 1 && !quiet {
		successCount := processedCount - failedCount
		prnt("processed %d repositories: %d successful, %d failed", processedCount, successCount, failedCount)
	}

	// Print the last successfully processed project directory
	if lastSuccessfulProjectDir != "" {
		abs, err := filepath.Abs(lastSuccessfulProjectDir)
		if err != nil {
			prnt("failed to get absolute path for %s: %s", lastSuccessfulProjectDir, err)
			fmt.Println(lastSuccessfulProjectDir) // fallback to relative path
		} else {
			fmt.Println(abs)
		}
	} else {
		// No successful repositories processed
		if !quiet {
			prnt("no repositories were successfully processed")
		}
		os.Exit(1)
	}
}

// normalize normalizes the given repository string and returns the parsed repository URL.
// Enhanced with security validation against path traversal attacks.
// Returns error instead of empty string for better error handling.
func normalize(repo string) (string, error) {
	if repo == "" {
		return "", errors.New("repository URL is empty")
	}

	r := regexPool.Get().(*regexp.Regexp)
	defer regexPool.Put(r)

	match := r.FindStringSubmatch(repo)
	if len(match) != 3 {
		return "", errors.New("failed to parse repository URL format")
	}

	host := match[1]
	path := match[2]

	// Security: Validate host component
	if !validHostname.MatchString(host) {
		return "", fmt.Errorf("invalid hostname: %s", host)
	}

	// Security: Sanitize path to prevent traversal attacks
	sanitizedPath, err := sanitizePathWithError(path)
	if err != nil {
		return "", fmt.Errorf("invalid repository path: %w", err)
	}

	// Security: Validate final path doesn't contain dangerous patterns
	if strings.Contains(sanitizedPath, "..") || strings.Contains(sanitizedPath, "//") {
		return "", errors.New("repository path contains dangerous patterns")
	}

	return filepath.Join(host, sanitizedPath), nil
}

// sanitizePathWithError cleans and validates repository paths against security threats
// Returns error for better error handling instead of empty string
func sanitizePathWithError(path string) (string, error) {
	if path == "" {
		return "", nil // Empty path is acceptable
	}

	originalPath := path

	// Remove dangerous prefixes and suffixes
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimPrefix(path, "~")
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")

	// Security: Check for path traversal attempts
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("path traversal detected: %s", originalPath)
	}

	// Security: Remove consecutive slashes
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}

	// Security: Validate path contains only safe characters
	if path != "" && !validRepoPath.MatchString(path) {
		return "", fmt.Errorf("path contains invalid characters: %s", originalPath)
	}

	// Security: Ensure path doesn't start with dangerous patterns
	dangerousPatterns := []string{".", "-", "_"}
	for _, pattern := range dangerousPatterns {
		if strings.HasPrefix(path, pattern+"/") {
			return "", fmt.Errorf("path starts with dangerous pattern '%s': %s", pattern, originalPath)
		}
	}

	return path, nil
}

// sanitizePath cleans and validates repository paths against security threats
func sanitizePath(path string) string {
	if path == "" {
		return ""
	}

	// Remove dangerous prefixes and suffixes
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimPrefix(path, "~")
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")

	// Security: Check for path traversal attempts
	if strings.Contains(path, "..") {
		return ""
	}

	// Security: Remove consecutive slashes
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}

	// Security: Validate path contains only safe characters
	if path != "" && !validRepoPath.MatchString(path) {
		return ""
	}

	// Security: Ensure path doesn't start with dangerous patterns
	dangerousPatterns := []string{".", "-", "_"}
	for _, pattern := range dangerousPatterns {
		if strings.HasPrefix(path, pattern+"/") {
			return ""
		}
	}

	return path
}

// getProjectDir returns the project directory based on the given repository URL.
// It retrieves the GIT_PROJECT_DIR environment variable and normalizes it.
// If the GIT_PROJECT_DIR starts with "~", it replaces it with the user's home directory.
// The normalized repository URL is then joined with the GIT_PROJECT_DIR to form the project directory path.
// Returns error for better error handling instead of empty string.
func getProjectDir(repository string) (string, error) {
	gitProjectDir := os.Getenv("GIT_PROJECT_DIR")
	
	if strings.HasPrefix(gitProjectDir, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		gitProjectDir = filepath.Join(homeDir, gitProjectDir[1:])
	}

	normalizedRepo, err := normalize(repository)
	if err != nil {
		return "", fmt.Errorf("failed to normalize repository URL: %w", err)
	}

	// Security: Validate and clean the final path
	projectDir := filepath.Join(gitProjectDir, normalizedRepo)
	cleanedPath := filepath.Clean(projectDir)
	
	// Security: Ensure the path doesn't escape the base directory
	if gitProjectDir != "" && !strings.HasPrefix(cleanedPath, filepath.Clean(gitProjectDir)) {
		return "", errors.New("security: path traversal detected in project directory")
	}

	return cleanedPath, nil
}

// isDirectoryNotEmptyRaw checks if the specified directory is not empty without caching.
// It uses the Readdirnames function to get the directory contents without loading full FileInfo
// structures for each entry. If there are any entries, it returns true. Otherwise, it returns false.
func isDirectoryNotEmptyRaw(name string) bool {
	f, err := os.Open(name)
	if err != nil {
		return false
	}

	names, err := f.Readdirnames(1)
	f.Close() // Direct call without defer for better performance

	return err == nil && len(names) > 0
}

// IsDirectoryNotEmpty checks if the specified directory is not empty with caching.
func (dc *DirCache) IsDirectoryNotEmpty(name string) bool {
	dc.mutex.RLock()
	if entry, ok := dc.cache[name]; ok {
		if time.Since(entry.timestamp) < dc.ttl {
			dc.mutex.RUnlock()
			return entry.exists
		}
	}
	dc.mutex.RUnlock()

	exists := isDirectoryNotEmptyRaw(name)

	dc.mutex.Lock()
	dc.cache[name] = cacheEntry{exists, time.Now()}
	dc.mutex.Unlock()

	return exists
}

// isDirectoryNotEmpty is a wrapper that uses the global cache.
func isDirectoryNotEmpty(name string) bool {
	return dirCache.IsDirectoryNotEmpty(name)
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
