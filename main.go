package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/pflag"
)

// Dependencies interfaces for better testability and modularity

// FileSystem abstracts file system operations for better testability
type FileSystem interface {
	Open(name string) (*os.File, error)
	Stat(name string) (os.FileInfo, error)
	MkdirAll(path string, perm os.FileMode) error
	UserHomeDir() (string, error)
	Getenv(key string) string
}

// CommandRunner abstracts command execution for better testability
type CommandRunner interface {
	LookPath(file string) (string, error)
	Run(ctx context.Context, name string, args ...string) error
	RunWithOutput(ctx context.Context, name string, args ...string) ([]byte, error)
}

// GitCloner abstracts git cloning operations for better testability
type GitCloner interface {
	Clone(ctx context.Context, repository, targetDir string, quiet, shallow bool) error
}

// DirectoryChecker abstracts directory existence checking for better testability
type DirectoryChecker interface {
	IsNotEmpty(name string) bool
}

// Environment abstracts environment operations
type Environment interface {
	UserHomeDir() (string, error)
	Getenv(key string) string
}

// Dependencies holds all external dependencies for the application
type Dependencies struct {
	FS       FileSystem
	CmdRun   CommandRunner
	GitClone GitCloner
	DirCheck DirectoryChecker
	Env      Environment
}

// Default implementations for production use

// DefaultFileSystem provides real file system operations
type DefaultFileSystem struct{}

func (fs *DefaultFileSystem) Open(name string) (*os.File, error) {
	return os.Open(name)
}

func (fs *DefaultFileSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (fs *DefaultFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (fs *DefaultFileSystem) UserHomeDir() (string, error) {
	return os.UserHomeDir()
}

func (fs *DefaultFileSystem) Getenv(key string) string {
	return os.Getenv(key)
}

// DefaultCommandRunner provides real command execution
type DefaultCommandRunner struct{}

func (cr *DefaultCommandRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (cr *DefaultCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Run()
}

func (cr *DefaultCommandRunner) RunWithOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}

// DefaultGitCloner provides real git cloning functionality
type DefaultGitCloner struct{}

func (gc *DefaultGitCloner) Clone(ctx context.Context, repository, targetDir string, quiet, shallow bool) error {
	return secureGitClone(ctx, repository, targetDir, quiet, shallow)
}

// DefaultDirectoryChecker provides real directory checking functionality
type DefaultDirectoryChecker struct {
	cache *DirCache
}

func NewDefaultDirectoryChecker(fs FileSystem) *DefaultDirectoryChecker {
	return &DefaultDirectoryChecker{
		cache: NewDirCache(DefaultCacheConfig(), fs),
	}
}

func NewDirectoryCheckerWithConfig(fs FileSystem, config *CacheConfig) *DefaultDirectoryChecker {
	return &DefaultDirectoryChecker{
		cache: NewDirCache(config, fs),
	}
}

func (dc *DefaultDirectoryChecker) IsNotEmpty(name string) bool {
	return dc.cache.IsDirectoryNotEmpty(name)
}

// DefaultEnvironment provides real environment operations
type DefaultEnvironment struct{}

func (env *DefaultEnvironment) UserHomeDir() (string, error) {
	return os.UserHomeDir()
}

func (env *DefaultEnvironment) Getenv(key string) string {
	return os.Getenv(key)
}

// NewDefaultDependencies creates a new Dependencies instance with default implementations
func NewDefaultDependencies() *Dependencies {
	fs := &DefaultFileSystem{}
	return &Dependencies{
		FS:       fs,
		CmdRun:   &DefaultCommandRunner{},
		GitClone: &DefaultGitCloner{},
		DirCheck: NewDefaultDirectoryChecker(fs),
		Env:      &DefaultEnvironment{},
	}
}

// NewDependenciesWithCacheConfig creates a new Dependencies instance with custom cache configuration
func NewDependenciesWithCacheConfig(cacheConfig *CacheConfig) *Dependencies {
	fs := &DefaultFileSystem{}
	return &Dependencies{
		FS:       fs,
		CmdRun:   &DefaultCommandRunner{},
		GitClone: &DefaultGitCloner{},
		DirCheck: NewDirectoryCheckerWithConfig(fs, cacheConfig),
		Env:      &DefaultEnvironment{},
	}
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Config holds the configuration for the application
type Config struct {
	ShowCommandHelp bool
	ShowVersionInfo bool
	Quiet           bool
	ShallowClone    bool
	Workers         int
	RepositoryArgs  []string
	Dependencies    *Dependencies
	CacheConfig     *CacheConfig
}

// ProcessingResult holds the result of repository processing
type ProcessingResult struct {
	LastSuccessfulProjectDir string
	ProcessedCount           int
	FailedCount              int
}

// RepositoryJob represents a job for cloning a repository
type RepositoryJob struct {
	Repository string
	Index      int // Original position in the arguments list
}

// WorkerResult represents the result of processing a repository job
type WorkerResult struct {
	Job        RepositoryJob
	ProjectDir string
	Success    bool
	Error      error
}

// WorkerPool manages parallel repository cloning
type WorkerPool struct {
	config       *Config
	jobs         chan RepositoryJob
	results      chan WorkerResult
	done         chan struct{}
	shutdown     chan struct{}
	shutdownOnce sync.Once
	workerCount  int32
}

// NewWorkerPool creates a new worker pool for parallel repository cloning
func NewWorkerPool(config *Config) *WorkerPool {
	return &WorkerPool{
		config:   config,
		jobs:     make(chan RepositoryJob, len(config.RepositoryArgs)),
		results:  make(chan WorkerResult, len(config.RepositoryArgs)),
		done:     make(chan struct{}),
		shutdown: make(chan struct{}),
	}
}

// worker is the worker goroutine that processes repository cloning jobs
func (wp *WorkerPool) worker(workerID int) {
	atomic.AddInt32(&wp.workerCount, 1)
	defer atomic.AddInt32(&wp.workerCount, -1)

	for {
		select {
		case job, ok := <-wp.jobs:
			if !ok {
				return // Jobs channel is closed
			}
			wp.processJob(job, workerID)
		case <-wp.shutdown:
			return // Shutdown requested
		}
	}
}

// processOneRepository validates, resolves, and clones a single repository.
// Returns the project directory and an error if any step fails.
func processOneRepository(config *Config, repository string) (string, error) {
	if err := validateRepositoryURL(repository); err != nil {
		return "", fmt.Errorf("invalid repository URL '%s': %w", repository, err)
	}

	projectDir, err := getProjectDir(repository, config.Dependencies.Env)
	if err != nil {
		return "", fmt.Errorf("failed to determine project directory for '%s': %w", repository, err)
	}

	if config.Dependencies.DirCheck.IsNotEmpty(projectDir) {
		if !config.Quiet {
			fmt.Fprintf(os.Stderr, "repository already exists: %s\n", projectDir)
		}
		return projectDir, nil
	}

	if err := config.Dependencies.FS.MkdirAll(filepath.Dir(projectDir), 0750); err != nil {
		return "", fmt.Errorf("failed create directory: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if err := config.Dependencies.GitClone.Clone(ctx, repository, filepath.Dir(projectDir), config.Quiet, config.ShallowClone); err != nil {
		return "", fmt.Errorf("failed clone repository '%s': %w", repository, err)
	}

	if !config.Quiet {
		fmt.Fprintln(os.Stderr)
	}

	return projectDir, nil
}

func (wp *WorkerPool) processJob(job RepositoryJob, _ int) {
	result := WorkerResult{Job: job}

	projectDir, err := processOneRepository(wp.config, job.Repository)
	if err != nil {
		result.Error = err
		wp.results <- result
		return
	}

	result.ProjectDir = projectDir
	result.Success = true
	wp.results <- result
}

// Start starts the worker pool and processes all repositories
func (wp *WorkerPool) Start() *ProcessingResult {
	// Start workers
	for i := 0; i < wp.config.Workers; i++ {
		go wp.worker(i)
	}

	// Send jobs to workers
	go func() {
		defer close(wp.jobs)
		for i, repository := range wp.config.RepositoryArgs {
			job := RepositoryJob{
				Repository: strings.TrimSpace(repository),
				Index:      i,
			}
			select {
			case wp.jobs <- job:
			case <-wp.shutdown:
				return // Shutdown requested, stop sending jobs
			}
		}
	}()

	// Collect results
	result := &ProcessingResult{}
	processedCount := 0
	expectedJobs := len(wp.config.RepositoryArgs)

	for processedCount < expectedJobs {
		select {
		case workerResult := <-wp.results:
			result.ProcessedCount++
			processedCount++

			if workerResult.Success {
				result.LastSuccessfulProjectDir = workerResult.ProjectDir
			} else {
				result.FailedCount++
				if workerResult.Error != nil {
					prntf(workerResult.Error.Error())
				}
			}
		case <-wp.shutdown:
			wp.gracefulShutdown()
			wp.waitForWorkers()
			// Drain remaining results from in-flight jobs
			for {
				select {
				case workerResult := <-wp.results:
					result.ProcessedCount++
					processedCount++
					if workerResult.Success {
						result.LastSuccessfulProjectDir = workerResult.ProjectDir
					} else {
						result.FailedCount++
						if workerResult.Error != nil {
							prntf(workerResult.Error.Error())
						}
					}
				default:
					return result
				}
			}
		}
	}

	// Signal completion and wait for workers to finish
	close(wp.done)
	wp.waitForWorkers()

	return result
}

// gracefulShutdown signals all workers to shut down
func (wp *WorkerPool) gracefulShutdown() {
	wp.shutdownOnce.Do(func() {
		close(wp.shutdown)
	})
}

// waitForWorkers waits for all workers to finish gracefully
func (wp *WorkerPool) waitForWorkers() {
	// Wait with timeout to avoid hanging indefinitely
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if atomic.LoadInt32(&wp.workerCount) == 0 {
				return // All workers have finished
			}
		case <-timeout:
			// Force shutdown after timeout
			return
		}
	}
}

// StartWithSignalHandling starts the worker pool with signal handling for graceful shutdown
func (wp *WorkerPool) StartWithSignalHandling() *ProcessingResult {
	// Set up signal handling
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	// Start worker pool in a goroutine
	resultChan := make(chan *ProcessingResult, 1)
	go func() {
		resultChan <- wp.Start()
	}()

	// Wait for either completion or signal
	select {
	case result := <-resultChan:
		// Normal completion
		signal.Stop(signalChan)
		return result
	case sig := <-signalChan:
		// Signal received, initiate graceful shutdown
		if !wp.config.Quiet {
			fmt.Fprintf(os.Stderr, "\nReceived signal %v, initiating graceful shutdown...\n", sig)
		}
		wp.gracefulShutdown()

		// Wait for result with timeout
		select {
		case result := <-resultChan:
			if !wp.config.Quiet {
				fmt.Fprintf(os.Stderr, "Graceful shutdown completed.\n")
			}
			return result
		case <-time.After(45 * time.Second):
			// Force shutdown if graceful shutdown takes too long
			if !wp.config.Quiet {
				fmt.Fprintf(os.Stderr, "Graceful shutdown timeout, forcing exit.\n")
			}
			os.Exit(1)
			return nil // Never reached
		}
	}
}

// getDefaultWorkers returns the default number of workers based on CPU count
func getDefaultWorkers() int {
	cpuCount := runtime.NumCPU()
	if cpuCount > 4 {
		return 4
	}
	return cpuCount
}

// RegexType represents different types of repository URL patterns
type RegexType int

const (
	RegexHTTPS RegexType = iota
	RegexSSH
	RegexGit
	RegexGeneric
)

var (
	httpsRegex   = regexp.MustCompile(`^https?://([^/]+)/(.+?)(?:\.git)?/?$`)
	sshRegex     = regexp.MustCompile(`^(?:ssh://)?([^@]+)@([^/:]+)(?::(\d+))?[:/](.+?)(?:\.git)?/?$`)
	gitRegex     = regexp.MustCompile(`^git://([^/]+)/(.+?)(?:\.git)?/?$`)
	genericRegex = regexp.MustCompile(`^(?:.*://)?(?:[^@]+@)?([^:/]+)(?::\d+)?[/:]?(.*)$`)
)

// CacheConfig holds configuration parameters for directory cache
type CacheConfig struct {
	TTL                   time.Duration
	CleanupInterval       time.Duration
	MaxEntries            int
	EnablePeriodicCleanup bool
}

// DefaultCacheConfig returns default cache configuration
func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		TTL:                   60 * time.Second, // Increased from 30s to 1 minute
		CleanupInterval:       5 * time.Minute,
		MaxEntries:            1000,
		EnablePeriodicCleanup: true,
	}
}

type cacheEntry struct {
	exists     bool
	timestamp  time.Time
	lastAccess time.Time
}

type DirCache struct {
	cache       map[string]cacheEntry
	mutex       sync.RWMutex
	config      *CacheConfig
	fs          FileSystem
	stopCleanup chan struct{}
	cleanupOnce sync.Once
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
	validRepoPath = regexp.MustCompile(`^[a-zA-Z0-9._/~-]+$`)
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

	// Validate hostname (strip port if present)
	if parsedURL.Host != "" {
		host := parsedURL.Host
		if strings.Contains(host, ":") {
			var err error
			host, _, err = net.SplitHostPort(host)
			if err != nil {
				return fmt.Errorf("invalid host:port: %s", parsedURL.Host)
			}
		}
		if !validHostname.MatchString(host) {
			return fmt.Errorf("invalid hostname: %s", host)
		}
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
	if len(path) > 0 && path[0] == '/' {
		// Remove leading slash for validation but allow it
		path = path[1:]
	}

	// Allow tilde for user directories but validate the rest
	if len(path) > 0 && path[0] == '~' {
		path = path[1:]
		if len(path) > 0 && path[0] == '/' {
			path = path[1:]
		}
	}

	// Validate remaining path characters (allow common Git repo path characters)
	if path != "" && !validRepoPath.MatchString(path) {
		return fmt.Errorf("invalid characters in repository path: %s", path)
	}

	return nil
}

// secureGitClone performs git clone with additional security measures
func secureGitClone(ctx context.Context, repository, targetDir string, quiet, shallow bool) error {
	if err := validateRepositoryURL(repository); err != nil {
		return fmt.Errorf("security validation failed: %w", err)
	}

	cleanTargetDir := filepath.Clean(targetDir)
	if strings.Contains(cleanTargetDir, "..") {
		return errors.New("target directory contains path traversal")
	}

	args := []string{"clone"}
	if shallow {
		args = append(args, "--depth=1")
	}
	args = append(args, "--", repository)
	cmd := exec.CommandContext(ctx, "git", args...)

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

// parseArgs parses command line arguments and returns configuration
func parseArgs() (*Config, error) {
	cacheConfig := DefaultCacheConfig()
	config := &Config{
		Dependencies: NewDependenciesWithCacheConfig(cacheConfig),
		CacheConfig:  cacheConfig,
		Workers:      getDefaultWorkers(),
	}

	pflag.BoolVarP(&config.ShowCommandHelp, "help", "h", false, "Show this help message and exit")
	pflag.BoolVarP(&config.ShowVersionInfo, "version", "v", false, "Show the version number and exit")
	pflag.BoolVarP(&config.Quiet, "quiet", "q", false, "Suppress output")
	pflag.BoolVarP(&config.ShallowClone, "shallow", "s", false, "Perform shallow clone with --depth=1")
	pflag.IntVarP(&config.Workers, "workers", "w", getDefaultWorkers(), "Number of parallel workers for cloning")
	pflag.Parse()

	config.RepositoryArgs = pflag.Args()

	// Validate workers count
	if config.Workers < 1 {
		return nil, errors.New("workers count must be at least 1")
	}
	if config.Workers > 32 {
		return nil, errors.New("workers count cannot exceed 32")
	}

	// Validate that we have arguments (unless help or version is requested)
	if !config.ShowCommandHelp && !config.ShowVersionInfo && len(config.RepositoryArgs) == 0 {
		return nil, errors.New("no repository URLs provided")
	}

	// Validate each argument is not empty
	for i, arg := range config.RepositoryArgs {
		if strings.TrimSpace(arg) == "" {
			return nil, fmt.Errorf("argument %d is empty", i+1)
		}
	}

	return config, nil
}

// validateDependencies checks if required dependencies are available
func validateDependencies(deps *Dependencies) error {
	// Check git availability with better error message
	if _, err := deps.CmdRun.LookPath("git"); err != nil {
		return errors.New("git command not found in PATH. Please install git: https://git-scm.com/downloads")
	}
	return nil
}

// processRepositories processes all repository arguments using worker pool and returns the result
func processRepositories(config *Config) *ProcessingResult {
	// For single repository or single worker, use sequential processing to avoid overhead
	if len(config.RepositoryArgs) == 1 || config.Workers == 1 {
		return processRepositoriesSequential(config)
	}

	// Use worker pool for multiple repositories with multiple workers
	wp := NewWorkerPool(config)
	return wp.StartWithSignalHandling()
}

func processRepositoriesSequential(config *Config) *ProcessingResult {
	result := &ProcessingResult{}

	for _, arg := range config.RepositoryArgs {
		repository := strings.TrimSpace(arg)
		result.ProcessedCount++

		projectDir, err := processOneRepository(config, repository)
		if err != nil {
			prntf("%s", err)
			result.FailedCount++
			continue
		}

		result.LastSuccessfulProjectDir = projectDir
	}

	return result
}

// printSummary prints the final summary and handles output/exit logic
func printSummary(config *Config, result *ProcessingResult) {
	// Print summary if multiple repositories were processed
	if result.ProcessedCount > 1 && !config.Quiet {
		successCount := result.ProcessedCount - result.FailedCount
		prntf("processed %d repositories: %d successful, %d failed",
			result.ProcessedCount, successCount, result.FailedCount)
	}

	// Print the last successfully processed project directory
	if result.LastSuccessfulProjectDir != "" {
		abs, err := filepath.Abs(result.LastSuccessfulProjectDir)
		if err != nil {
			prntf("failed to get absolute path for %s: %s", result.LastSuccessfulProjectDir, err)
			fmt.Println(result.LastSuccessfulProjectDir) // fallback to relative path
		} else {
			fmt.Println(abs)
		}
	} else {
		// No successful repositories processed
		if !config.Quiet {
			prntf("no repositories were successfully processed")
		}
		os.Exit(1)
	}
}

func main() {
	// Parse command line arguments
	config, err := parseArgs()
	if err != nil {
		prntf("error: %s", err)
		usage()
		os.Exit(1)
	}

	// Handle help command
	if config.ShowCommandHelp {
		usage()
		return
	}

	// Handle version command
	if config.ShowVersionInfo {
		if commit != "none" {
			fmt.Printf("gclone version %s, commit %s, built at %s\n", version, commit, date)
		} else {
			fmt.Printf("gclone version %s\n", version)
		}
		return
	}

	// Validate dependencies
	if err := validateDependencies(config.Dependencies); err != nil {
		prntf("error: %s", err)
		os.Exit(1)
	}

	// Process repositories
	result := processRepositories(config)

	// Print summary and handle exit
	printSummary(config, result)
}

// URLCache provides caching for normalized URLs to avoid repeated parsing
type URLCache struct {
	cache      map[string]string
	mutex      sync.RWMutex
	maxEntries int
}

var urlCache = &URLCache{
	cache:      make(map[string]string),
	maxEntries: 1000,
}

// Get retrieves a cached normalized URL
func (uc *URLCache) Get(key string) (string, bool) {
	uc.mutex.RLock()
	defer uc.mutex.RUnlock()
	value, exists := uc.cache[key]
	return value, exists
}

// Set stores a normalized URL in cache
func (uc *URLCache) Set(key, value string) {
	uc.mutex.Lock()
	defer uc.mutex.Unlock()

	// Simple eviction strategy: clear cache when full
	if len(uc.cache) >= uc.maxEntries {
		uc.cache = make(map[string]string)
	}
	uc.cache[key] = value
}

// Clear removes all entries from the URLCache for test isolation
func (uc *URLCache) Clear() {
	uc.mutex.Lock()
	defer uc.mutex.Unlock()
	uc.cache = make(map[string]string)
}

// detectRegexType determines the best regex pattern for the given URL
func detectRegexType(repo string) RegexType {
	// Check for HTTPS/HTTP URLs first
	if strings.HasPrefix(repo, "https://") || strings.HasPrefix(repo, "http://") {
		return RegexHTTPS
	}
	// Check for Git protocol URLs
	if strings.HasPrefix(repo, "git://") {
		return RegexGit
	}
	// Check for SSH URLs (ssh:// prefix or git@host:path format)
	if strings.HasPrefix(repo, "ssh://") {
		return RegexSSH
	}
	if strings.Contains(repo, "@") && strings.Contains(repo, ":") && !strings.Contains(repo, "://") {
		return RegexSSH
	}
	return RegexGeneric
}

func regexForType(regexType RegexType) *regexp.Regexp {
	switch regexType {
	case RegexHTTPS:
		return httpsRegex
	case RegexSSH:
		return sshRegex
	case RegexGit:
		return gitRegex
	default:
		return genericRegex
	}
}

func normalize(repo string) (string, error) {
	if repo == "" {
		return "", errors.New("repository URL is empty")
	}

	if cached, exists := urlCache.Get(repo); exists {
		return cached, nil
	}

	regexType := detectRegexType(repo)
	r := regexForType(regexType)

	var host, path string

	switch regexType {
	case RegexHTTPS, RegexGit:
		match := r.FindStringSubmatch(repo)
		if len(match) != 3 {
			return "", errors.New("failed to parse HTTPS/Git repository URL format")
		}
		host, path = match[1], match[2]

	case RegexSSH:
		match := r.FindStringSubmatch(repo)
		if len(match) != 5 {
			return "", errors.New("failed to parse SSH repository URL format")
		}
		// match[1] = user, match[2] = host, match[3] = port (optional), match[4] = path
		// For hostname validation, we only use the host part (without port)
		host, path = match[2], match[4]

	default: // RegexGeneric
		match := r.FindStringSubmatch(repo)
		if len(match) != 3 {
			return "", errors.New("failed to parse repository URL format")
		}
		host, path = match[1], match[2]
	}

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

	result := filepath.Join(host, sanitizedPath)

	// Cache the result
	urlCache.Set(repo, result)

	return result, nil
}

// sanitizePathWithError cleans and validates repository paths against security threats
// Returns error for better error handling instead of empty string
func sanitizePathWithError(path string) (string, error) {
	if path == "" {
		return "", nil // Empty path is acceptable
	}

	originalPath := path

	// Remove dangerous prefixes and suffixes with optimized string operations
	// Use string slicing to avoid multiple allocations
	for {
		originalLen := len(path)

		// Remove leading slashes and tildes
		if len(path) > 0 && (path[0] == '/' || path[0] == '~') {
			path = path[1:]
			continue
		}

		// Remove trailing slashes
		if len(path) > 0 && path[len(path)-1] == '/' {
			path = path[:len(path)-1]
			continue
		}

		// Remove .git suffix
		if len(path) >= 4 && path[len(path)-4:] == ".git" {
			path = path[:len(path)-4]
			continue
		}

		// If no changes were made, break
		if len(path) == originalLen {
			break
		}
	}

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
	if len(path) >= 2 {
		firstChar := path[0]
		if (firstChar == '.' || firstChar == '-' || firstChar == '_') && path[1] == '/' {
			return "", fmt.Errorf("path starts with dangerous pattern '%c/': %s", firstChar, originalPath)
		}
	}

	return path, nil
}

// getProjectDir returns the project directory based on the given repository URL.
// It retrieves the GIT_PROJECT_DIR environment variable and normalizes it.
// If the GIT_PROJECT_DIR starts with "~", it replaces it with the user's home directory.
// The normalized repository URL is then joined with the GIT_PROJECT_DIR to form the project directory path.
// Returns error for better error handling instead of empty string.
func getProjectDir(repository string, env Environment) (string, error) {
	gitProjectDir := env.Getenv("GIT_PROJECT_DIR")

	if len(gitProjectDir) > 0 && gitProjectDir[0] == '~' {
		homeDir, err := env.UserHomeDir()
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
	if gitProjectDir != "" {
		cleanGitProjectDir := filepath.Clean(gitProjectDir)
		if len(cleanedPath) < len(cleanGitProjectDir) || cleanedPath[:len(cleanGitProjectDir)] != cleanGitProjectDir {
			return "", errors.New("security: path traversal detected in project directory")
		}
	}

	return cleanedPath, nil
}

// isDirectoryNotEmptyRaw checks if the specified directory is not empty without caching.
// It uses the Readdirnames function to get the directory contents without loading full FileInfo
// structures for each entry. If there are any entries, it returns true. Otherwise, it returns false.
func isDirectoryNotEmptyRaw(name string, fs FileSystem) bool {
	f, err := fs.Open(name)
	if err != nil {
		return false
	}

	names, err := f.Readdirnames(1)
	f.Close() // Direct call without defer for better performance

	return err == nil && len(names) > 0
}

// NewDirCache creates a new directory cache with the given configuration
func NewDirCache(config *CacheConfig, fs FileSystem) *DirCache {
	if config == nil {
		config = DefaultCacheConfig()
	}

	dc := &DirCache{
		cache:       make(map[string]cacheEntry),
		config:      config,
		fs:          fs,
		stopCleanup: make(chan struct{}),
	}

	// Start periodic cleanup if enabled
	if config.EnablePeriodicCleanup {
		go dc.startPeriodicCleanup()
	}

	return dc
}

// Close stops the cache cleanup routine
func (dc *DirCache) Close() {
	dc.cleanupOnce.Do(func() {
		close(dc.stopCleanup)
	})
}

// startPeriodicCleanup runs periodic cleanup of expired cache entries
func (dc *DirCache) startPeriodicCleanup() {
	ticker := time.NewTicker(dc.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			dc.cleanup()
		case <-dc.stopCleanup:
			return
		}
	}
}

// cleanup removes expired entries from the cache
func (dc *DirCache) cleanup() {
	now := time.Now()
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	evictionCount := 0
	for key, entry := range dc.cache {
		if now.Sub(entry.timestamp) > dc.config.TTL {
			delete(dc.cache, key)
			evictionCount++
		}
	}
}

// Clear removes all entries from the cache
func (dc *DirCache) Clear() {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()

	dc.cache = make(map[string]cacheEntry)
}

// IsDirectoryNotEmpty checks if the specified directory is not empty with caching.
func (dc *DirCache) IsDirectoryNotEmpty(name string) bool {
	now := time.Now()

	// Try to get from cache first (optimistic read)
	dc.mutex.RLock()
	if entry, ok := dc.cache[name]; ok {
		if now.Sub(entry.timestamp) < dc.config.TTL {
			// Cache hit - we need to upgrade to write lock to update lastAccess
			dc.mutex.RUnlock()
			dc.mutex.Lock()
			// Double-check after acquiring write lock (entry might have been evicted)
			if entry, ok := dc.cache[name]; ok && now.Sub(entry.timestamp) < dc.config.TTL {
				entry.lastAccess = now
				dc.cache[name] = entry
				dc.mutex.Unlock()
				return entry.exists
			}
			dc.mutex.Unlock()
			// Entry was evicted or expired during lock upgrade, fall through to miss handling
		} else {
			dc.mutex.RUnlock()
		}
	} else {
		dc.mutex.RUnlock()
	}

	// Cache miss or expired entry - check directory
	exists := isDirectoryNotEmptyRaw(name, dc.fs)

	// Update cache with new entry
	dc.mutex.Lock()
	dc.cache[name] = cacheEntry{
		exists:     exists,
		timestamp:  now,
		lastAccess: now,
	}
	// Check if cache size exceeds limit and evict LRU entries if needed
	if dc.config.MaxEntries > 0 && len(dc.cache) > dc.config.MaxEntries {
		dc.evictLRU()
	}

	dc.mutex.Unlock()

	return exists
}

// evictLRU removes the least recently used entries to stay within MaxEntries limit
func (dc *DirCache) evictLRU() {
	// Find entries to evict (remove 10% of cache when limit is exceeded)
	targetSize := int(float64(dc.config.MaxEntries) * 0.9)
	toEvict := len(dc.cache) - targetSize

	if toEvict <= 0 {
		return
	}

	// Create slice of entries with their keys for sorting
	type entryWithKey struct {
		key        string
		lastAccess time.Time
	}

	entries := make([]entryWithKey, 0, len(dc.cache))
	for key, entry := range dc.cache {
		entries = append(entries, entryWithKey{key: key, lastAccess: entry.lastAccess})
	}

	// Sort by last access time (oldest first) using efficient sort.Slice
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastAccess.Before(entries[j].lastAccess)
	})

	// Remove oldest entries
	for i := 0; i < toEvict && i < len(entries); i++ {
		delete(dc.cache, entries[i].key)
	}
}

// Usage prints the usage of the program.
func usage() {
	fmt.Println("usage: gclone [-h] [-v] [-q] [-s] [-w WORKERS] [REPOSITORY]")
	fmt.Println()
	fmt.Println("positional arguments:")
	fmt.Println("  REPOSITORY         Repository URL")
	fmt.Println()
	fmt.Println("optional arguments:")
	fmt.Println("  -h, --help         Show this help message and exit")
	fmt.Println("  -v, --version      Show the version number and exit")
	fmt.Println("  -q, --quiet        Suppress output")
	fmt.Println("  -s, --shallow      Perform shallow clone with --depth=1")
	fmt.Printf("  -w, --workers      Number of parallel workers (default: %d)\n", getDefaultWorkers())
	fmt.Println()
	fmt.Println("environment variables:")
	fmt.Println("  GIT_PROJECT_DIR    Directory to clone repositories")
	fmt.Println()
	fmt.Println("examples:")
	fmt.Println("  GIT_PROJECT_DIR=\"$HOME/src\" gclone https://github.com/user/repo")
	fmt.Println("  gclone -w 8 https://github.com/user/repo1 https://github.com/user/repo2")
}

func prntf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	fmt.Fprintln(os.Stderr)
}
