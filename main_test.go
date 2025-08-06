package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func Test_getProjectDir(t *testing.T) {
	tests := []struct {
		name          string
		repository    string
		homeVar       string
		gitProjectDir string
		want          string
	}{
		{
			name:          "~/src",
			repository:    "https://github.com/user/repo",
			homeVar:       "/home/test",
			gitProjectDir: "~/src",
			want:          "/home/test/src/github.com/user/repo",
		},
		{
			name:          "~src",
			repository:    "https://github.com/user/repo",
			homeVar:       "/home/test",
			gitProjectDir: "~src",
			want:          "/home/test/src/github.com/user/repo",
		},
		{
			name:          "~/src",
			repository:    "https://github.com/user/repo",
			homeVar:       "/home/test",
			gitProjectDir: "~/src/~user",
			want:          "/home/test/src/~user/github.com/user/repo",
		},
		{
			name:          "src",
			repository:    "https://github.com/user/repo",
			homeVar:       "/home/test",
			gitProjectDir: "src",
			want:          "src/github.com/user/repo",
		},
		{
			name:          "src/tests",
			repository:    "https://github.com/user/repo",
			homeVar:       "/home/test",
			gitProjectDir: "src/tests",
			want:          "src/tests/github.com/user/repo",
		},
		{
			name:          "empty",
			repository:    "https://github.com/user/repo",
			homeVar:       "/home/test",
			gitProjectDir: "",
			want:          "github.com/user/repo",
		},
		{
			name:          "src",
			repository:    "ssh://user@host.xz:443/~user/path/to/repo.git/",
			homeVar:       "/home/test",
			gitProjectDir: "src",
			want:          "src/host.xz/user/path/to/repo",
		},
		{
			name:          "src",
			repository:    "git@github.com:go-git/go-git.git",
			homeVar:       "/home/test",
			gitProjectDir: "src",
			want:          "src/github.com/go-git/go-git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", tt.homeVar)
			t.Setenv("GIT_PROJECT_DIR", tt.gitProjectDir)

			env := &DefaultEnvironment{}
			got, err := getProjectDir(tt.repository, env)
			if err != nil {
				t.Errorf("getProjectDir() unexpected error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("getProjectDir() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isDirectoryNotEmpty(t *testing.T) {
	type args struct {
		name string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "notExist",
			args: args{
				name: "notExist",
			},
			want: false,
		},
		{
			name: "empty",
			args: args{
				name: "empty",
			},
			want: false,
		},
		{
			name: "nonEmpty",
			args: args{
				name: "nonEmpty",
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDirectoryNotEmpty(filepath.Join("testdata", tt.args.name))
			if got != tt.want {
				t.Errorf("isDirectoryNotEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_normalize(t *testing.T) {
	type args struct {
		repository string
	}
	tests := []struct {
		name     string
		args     args
		wantRepo string
	}{
		{
			name: "https",
			args: args{
				repository: "https://github.com/user/repo",
			},
			wantRepo: "github.com/user/repo",
		},
		{
			name: "without scheme",
			args: args{
				repository: "github.com/user/repo",
			},
			wantRepo: "github.com/user/repo",
		},
		{
			name: "ssh",
			args: args{
				repository: "ssh://user@host.xz:443/~user/path/to/repo.git/",
			},
			wantRepo: "host.xz/user/path/to/repo",
		},
		{
			name: "git",
			args: args{
				repository: "git@git.sr.ht:~libreboot/lbmk",
			},
			wantRepo: "git.sr.ht/libreboot/lbmk",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRepo, err := normalize(tt.args.repository)
			if err != nil {
				t.Errorf("normalize() unexpected error = %v", err)
				return
			}
			if gotRepo != tt.wantRepo {
				t.Errorf("normalize() gotRepo = %v, want %v", gotRepo, tt.wantRepo)
			}
		})
	}
}

// Benchmark functions
func BenchmarkNormalize(b *testing.B) {
	repository := "https://github.com/user/repo"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = normalize(repository)
	}
}

func BenchmarkIsDirectoryNotEmpty(b *testing.B) {
	// Create test directory
	tempDir, err := os.MkdirTemp("", "benchmark-test")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create a non-empty directory
	nonEmptyDir := filepath.Join(tempDir, "non-empty")
	if err := os.Mkdir(nonEmptyDir, 0755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonEmptyDir, "test.txt"), []byte("test"), 0644); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isDirectoryNotEmpty(nonEmptyDir)
	}
}

func BenchmarkIsDirectoryNotEmptyRaw(b *testing.B) {
	// Create test directory
	tempDir, err := os.MkdirTemp("", "benchmark-test")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create a non-empty directory
	nonEmptyDir := filepath.Join(tempDir, "non-empty")
	if err := os.Mkdir(nonEmptyDir, 0755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonEmptyDir, "test.txt"), []byte("test"), 0644); err != nil {
		b.Fatal(err)
	}

	fs := &DefaultFileSystem{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isDirectoryNotEmptyRaw(nonEmptyDir, fs)
	}
}

func BenchmarkIsDirectoryNotEmptyCache(b *testing.B) {
	// Create test directory
	tempDir, err := os.MkdirTemp("", "benchmark-test")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create a non-empty directory
	nonEmptyDir := filepath.Join(tempDir, "non-empty")
	if err := os.Mkdir(nonEmptyDir, 0755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonEmptyDir, "test.txt"), []byte("test"), 0644); err != nil {
		b.Fatal(err)
	}

	// Clear cache before benchmark
	dirCache.mutex.Lock()
	dirCache.cache = make(map[string]cacheEntry)
	dirCache.mutex.Unlock()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dirCache.IsDirectoryNotEmpty(nonEmptyDir) // This will benefit from caching after first call
	}
}

// New comprehensive benchmarks for performance measurement

// BenchmarkNormalizeHTTPS benchmarks HTTPS URL parsing
func BenchmarkNormalizeHTTPS(b *testing.B) {
	repository := "https://github.com/user/repo.git"
	// Clear cache before benchmark
	urlCache.cache = make(map[string]string)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = normalize(repository)
	}
}

// BenchmarkNormalizeSSH benchmarks SSH URL parsing
func BenchmarkNormalizeSSH(b *testing.B) {
	repository := "git@github.com:user/repo.git"
	// Clear cache before benchmark
	urlCache.cache = make(map[string]string)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = normalize(repository)
	}
}

// BenchmarkNormalizeGit benchmarks Git protocol URL parsing
func BenchmarkNormalizeGit(b *testing.B) {
	repository := "git://github.com/user/repo.git"
	// Clear cache before benchmark  
	urlCache.cache = make(map[string]string)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = normalize(repository)
	}
}

// BenchmarkNormalizeCached benchmarks cached URL parsing
func BenchmarkNormalizeCached(b *testing.B) {
	repository := "https://github.com/user/repo.git"
	// Warm up cache
	_, _ = normalize(repository)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = normalize(repository)
	}
}

// BenchmarkNormalizeMixed benchmarks mixed URL types
func BenchmarkNormalizeMixed(b *testing.B) {
	repositories := []string{
		"https://github.com/user/repo1.git",
		"git@github.com:user/repo2.git", 
		"git://github.com/user/repo3.git",
		"https://gitlab.com/user/repo4.git",
		"git@gitlab.com:user/repo5.git",
	}
	// Clear cache before benchmark
	urlCache.cache = make(map[string]string)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = normalize(repositories[i%len(repositories)])
	}
}

// BenchmarkSanitizePathOptimized benchmarks optimized path sanitization
func BenchmarkSanitizePathOptimized(b *testing.B) {
	path := "///~user//repo//.git///"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sanitizePath(path)
	}
}

// BenchmarkDetectRegexType benchmarks regex type detection
func BenchmarkDetectRegexType(b *testing.B) {
	urls := []string{
		"https://github.com/user/repo.git",
		"git@github.com:user/repo.git", 
		"git://github.com/user/repo.git",
		"github.com/user/repo",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = detectRegexType(urls[i%len(urls)])
	}
}

// Security tests
func TestValidateRepositoryURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid https URL",
			url:     "https://github.com/user/repo.git",
			wantErr: false,
		},
		{
			name:    "valid SSH URL",
			url:     "git@github.com:user/repo.git",
			wantErr: false,
		},
		{
			name:    "command injection attempt",
			url:     "https://github.com/user/repo.git; rm -rf /",
			wantErr: true,
			errMsg:  "dangerous characters",
		},
		{
			name:    "path traversal in URL",
			url:     "https://github.com/../../../etc/passwd",
			wantErr: true,
			errMsg:  "path traversal",
		},
		{
			name:    "invalid scheme",
			url:     "ftp://github.com/user/repo",
			wantErr: true,
			errMsg:  "unsupported URL scheme",
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name:    "backticks for command substitution",
			url:     "https://github.com/user/`whoami`.git",
			wantErr: true,
			errMsg:  "dangerous characters",
		},
		{
			name:    "pipe character",
			url:     "https://github.com/user/repo | cat /etc/passwd",
			wantErr: true,
			errMsg:  "dangerous characters",
		},
		{
			name:    "invalid hostname",
			url:     "https://github..com/user/repo",
			wantErr: true,
			errMsg:  "invalid hostname",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRepositoryURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateRepositoryURL() expected error but got none for URL: %s", tt.url)
					return
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateRepositoryURL() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateRepositoryURL() unexpected error = %v for URL: %s", err, tt.url)
				}
			}
		})
	}
}

func TestNormalizeSecurity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string // Empty string means should be rejected
	}{
		{
			name:     "normal repo",
			input:    "https://github.com/user/repo",
			expected: "github.com/user/repo",
		},
		{
			name:     "path traversal attempt",
			input:    "https://github.com/../../../etc/passwd",
			expected: "", // Should be rejected
		},
		{
			name:     "double slash",
			input:    "https://github.com//user//repo",
			expected: "github.com/user/repo",
		},
		{
			name:     "invalid hostname",
			input:    "https://github..com/user/repo",
			expected: "", // Should be rejected
		},
		{
			name:     "dangerous path start with dot",
			input:    "https://github.com/./repo",
			expected: "", // Should be rejected
		},
		{
			name:     "invalid characters in path",
			input:    "https://github.com/user/repo<script>",
			expected: "", // Should be rejected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalize(tt.input)
			if tt.expected == "" {
				// Expecting failure
				if err == nil {
					t.Errorf("normalize() expected error but got none for input: %s", tt.input)
					return
				}
			} else {
				// Expecting success
				if err != nil {
					t.Errorf("normalize() unexpected error = %v for input: %s", err, tt.input)
					return
				}
				if result != tt.expected {
					t.Errorf("normalize() = %v, want %v", result, tt.expected)
				}
			}
		})
	}
}

func TestSecureGitCloneTimeout(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "git-clone-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name        string
		repository  string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "dangerous URL should fail validation", 
			repository:  "https://github.com/user/repo; rm -rf /",
			expectError: true,
			errorMsg:    "security validation failed",
		},
		{
			name:        "path traversal should fail validation", 
			repository:  "https://github.com/../../../etc/passwd",
			expectError: true,
			errorMsg:    "security validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := secureGitClone(tt.repository, tempDir, true)
			if tt.expectError {
				if err == nil {
					t.Errorf("secureGitClone() expected error but got none for repository: %s", tt.repository)
					return
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("secureGitClone() error = %v, expected to contain %v", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("secureGitClone() unexpected error = %v for repository: %s", err, tt.repository)
				}
			}
		})
	}
}

func TestSanitizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal path",
			input:    "user/repo",
			expected: "user/repo",
		},
		{
			name:     "path with traversal",
			input:    "user/../../../etc/passwd",
			expected: "", // Should be rejected
		},
		{
			name:     "path with double slashes",
			input:    "user//repo",
			expected: "user/repo",
		},
		{
			name:     "path starting with dot",
			input:    "./user/repo",
			expected: "", // Should be rejected
		},
		{
			name:     "path with git suffix",
			input:    "user/repo.git",
			expected: "user/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizePath(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizePath() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetProjectDirSecurity(t *testing.T) {
	// Save original env var
	originalGitProjectDir := os.Getenv("GIT_PROJECT_DIR")
	defer func() {
		if originalGitProjectDir != "" {
			os.Setenv("GIT_PROJECT_DIR", originalGitProjectDir)
		} else {
			os.Unsetenv("GIT_PROJECT_DIR")
		}
	}()

	tests := []struct {
		name           string
		gitProjectDir  string
		repository     string
		expectedResult string // Empty means should be rejected
	}{
		{
			name:           "normal case",
			gitProjectDir:  "/tmp/test",
			repository:     "https://github.com/user/repo",
			expectedResult: "/tmp/test/github.com/user/repo",
		},
		{
			name:           "path traversal in repo",
			gitProjectDir:  "/tmp/test",
			repository:     "https://github.com/../../etc/passwd",
			expectedResult: "", // Should be rejected due to normalization failure
		},
		{
			name:           "invalid repo URL",
			gitProjectDir:  "/tmp/test",
			repository:     "https://github..com/user/repo",
			expectedResult: "", // Should be rejected due to invalid hostname
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("GIT_PROJECT_DIR", tt.gitProjectDir)
			env := &DefaultEnvironment{}
			result, err := getProjectDir(tt.repository, env)
			if tt.expectedResult == "" {
				// Expecting failure
				if err == nil {
					t.Errorf("getProjectDir() expected error but got none for repository: %s", tt.repository)
					return
				}
			} else {
				// Expecting success
				if err != nil {
					t.Errorf("getProjectDir() unexpected error = %v for repository: %s", err, tt.repository)
					return
				}
				if result != tt.expectedResult {
					t.Errorf("getProjectDir() = %v, want %v", result, tt.expectedResult)
				}
			}
		})
	}
}

// Tests for logical fixes
func TestSanitizePathWithError(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "normal path",
			input:    "user/repo",
			expected: "user/repo",
			wantErr:  false,
		},
		{
			name:     "path with traversal",
			input:    "user/../../../etc/passwd",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "path with double slashes",
			input:    "user//repo",
			expected: "user/repo",
			wantErr:  false,
		},
		{
			name:     "path starting with dot",
			input:    "./user/repo",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "path with git suffix",
			input:    "user/repo.git",
			expected: "user/repo",
			wantErr:  false,
		},
		{
			name:     "path with invalid characters",
			input:    "user/repo<script>",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "empty path",
			input:    "",
			expected: "",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := sanitizePathWithError(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("sanitizePathWithError() expected error but got none for input: %s", tt.input)
					return
				}
			} else {
				if err != nil {
					t.Errorf("sanitizePathWithError() unexpected error = %v for input: %s", err, tt.input)
					return
				}
				if result != tt.expected {
					t.Errorf("sanitizePathWithError() = %v, want %v", result, tt.expected)
				}
			}
		})
	}
}

// Mock implementations for worker pool testing

// MockGitCloner is a mock implementation of GitCloner interface
type MockGitCloner struct {
	ShouldFail bool
	CallCount  int
	mutex      sync.Mutex
}

func (m *MockGitCloner) Clone(ctx context.Context, repository, targetDir string, quiet bool) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.CallCount++
	if m.ShouldFail {
		return errors.New("mock clone failed")
	}
	return nil
}

func (m *MockGitCloner) GetCallCount() int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.CallCount
}

// MockDirectoryChecker is a mock implementation of DirectoryChecker interface
type MockDirectoryChecker struct {
	ExistingDirs map[string]bool
}

func (m *MockDirectoryChecker) IsNotEmpty(name string) bool {
	return m.ExistingDirs[name]
}

// MockFileSystem is a mock implementation of FileSystem interface
type MockFileSystem struct {
	ShouldFailMkdir bool
}

func (m *MockFileSystem) Open(name string) (*os.File, error) {
	return nil, errors.New("mock open not implemented")
}

func (m *MockFileSystem) Stat(name string) (os.FileInfo, error) {
	return nil, errors.New("mock stat not implemented")
}

func (m *MockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	if m.ShouldFailMkdir {
		return errors.New("mock mkdir failed")
	}
	return nil
}

func (m *MockFileSystem) UserHomeDir() (string, error) {
	return "/home/test", nil
}

func (m *MockFileSystem) Getenv(key string) string {
	if key == "GIT_PROJECT_DIR" {
		return "/tmp/test"
	}
	return ""
}

// Test functions for worker pool

func TestGetDefaultWorkers(t *testing.T) {
	workers := getDefaultWorkers()
	if workers < 1 {
		t.Errorf("getDefaultWorkers() = %d, expected at least 1", workers)
	}
	if workers > 4 {
		t.Errorf("getDefaultWorkers() = %d, expected at most 4", workers)
	}
}

func TestWorkerPoolBasicFunctionality(t *testing.T) {
	// Setup mock dependencies
	mockGitCloner := &MockGitCloner{ShouldFail: false}
	mockDirChecker := &MockDirectoryChecker{ExistingDirs: make(map[string]bool)}
	mockFS := &MockFileSystem{ShouldFailMkdir: false}
	mockEnv := &DefaultEnvironment{}

	config := &Config{
		Workers: 2,
		Quiet:   true,
		RepositoryArgs: []string{
			"https://github.com/user/repo1",
			"https://github.com/user/repo2",
		},
		Dependencies: &Dependencies{
			FS:       mockFS,
			GitClone: mockGitCloner,
			DirCheck: mockDirChecker,
			Env:      mockEnv,
		},
	}

	// Test worker pool creation
	wp := NewWorkerPool(config)
	if wp == nil {
		t.Fatal("NewWorkerPool() returned nil")
	}

	// Test that channels are created
	if wp.jobs == nil || wp.results == nil || wp.done == nil || wp.shutdown == nil {
		t.Error("NewWorkerPool() did not create all required channels")
	}

	// Test buffer sizes
	if cap(wp.jobs) != len(config.RepositoryArgs) {
		t.Errorf("jobs channel buffer size = %d, expected %d", cap(wp.jobs), len(config.RepositoryArgs))
	}
	if cap(wp.results) != len(config.RepositoryArgs) {
		t.Errorf("results channel buffer size = %d, expected %d", cap(wp.results), len(config.RepositoryArgs))
	}
}

func TestWorkerPoolSequentialFallback(t *testing.T) {
	mockGitCloner := &MockGitCloner{ShouldFail: false}
	mockDirChecker := &MockDirectoryChecker{ExistingDirs: make(map[string]bool)}
	mockFS := &MockFileSystem{ShouldFailMkdir: false}
	mockEnv := &DefaultEnvironment{}

	// Test single repository (should use sequential processing)
	config := &Config{
		Workers: 4,
		Quiet:   true,
		RepositoryArgs: []string{
			"https://github.com/user/repo1",
		},
		Dependencies: &Dependencies{
			FS:       mockFS,
			GitClone: mockGitCloner,
			DirCheck: mockDirChecker,
			Env:      mockEnv,
		},
	}

	result := processRepositories(config)

	if result.ProcessedCount != 1 {
		t.Errorf("ProcessedCount = %d, expected 1", result.ProcessedCount)
	}

	if result.FailedCount != 0 {
		t.Errorf("FailedCount = %d, expected 0", result.FailedCount)
	}

	// Test single worker (should use sequential processing)
	config.Workers = 1
	config.RepositoryArgs = []string{
		"https://github.com/user/repo1",
		"https://github.com/user/repo2",
	}

	// Reset mock
	mockGitCloner = &MockGitCloner{ShouldFail: false}
	config.Dependencies.GitClone = mockGitCloner

	result = processRepositories(config)

	if result.ProcessedCount != 2 {
		t.Errorf("ProcessedCount = %d, expected 2", result.ProcessedCount)
	}

	if result.FailedCount != 0 {
		t.Errorf("FailedCount = %d, expected 0", result.FailedCount)
	}
}

// Benchmarks for comparing sequential vs parallel performance

func BenchmarkSequentialProcessing(b *testing.B) {
	mockGitCloner := &MockGitCloner{ShouldFail: false}
	mockDirChecker := &MockDirectoryChecker{ExistingDirs: make(map[string]bool)}
	mockFS := &MockFileSystem{ShouldFailMkdir: false}
	mockEnv := &DefaultEnvironment{}

	// Create test URLs
	testRepos := make([]string, 10)
	for i := 0; i < 10; i++ {
		testRepos[i] = fmt.Sprintf("https://github.com/user/repo%d", i)
	}

	config := &Config{
		Workers: 1, // Force sequential processing
		Quiet:   true,
		RepositoryArgs: testRepos,
		Dependencies: &Dependencies{
			FS:       mockFS,
			GitClone: mockGitCloner,
			DirCheck: mockDirChecker,
			Env:      mockEnv,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset mock for each iteration
		mockGitCloner = &MockGitCloner{ShouldFail: false}
		config.Dependencies.GitClone = mockGitCloner
		
		result := processRepositories(config)
		if result.ProcessedCount != 10 {
			b.Errorf("Expected 10 processed, got %d", result.ProcessedCount)
		}
	}
}

func BenchmarkParallelProcessing(b *testing.B) {
	mockGitCloner := &MockGitCloner{ShouldFail: false}
	mockDirChecker := &MockDirectoryChecker{ExistingDirs: make(map[string]bool)}
	mockFS := &MockFileSystem{ShouldFailMkdir: false}
	mockEnv := &DefaultEnvironment{}

	// Create test URLs
	testRepos := make([]string, 10)
	for i := 0; i < 10; i++ {
		testRepos[i] = fmt.Sprintf("https://github.com/user/repo%d", i)
	}

	config := &Config{
		Workers: 4, // Use parallel processing
		Quiet:   true,
		RepositoryArgs: testRepos,
		Dependencies: &Dependencies{
			FS:       mockFS,
			GitClone: mockGitCloner,
			DirCheck: mockDirChecker,
			Env:      mockEnv,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset mock for each iteration
		mockGitCloner = &MockGitCloner{ShouldFail: false}
		config.Dependencies.GitClone = mockGitCloner
		
		result := processRepositories(config)
		if result.ProcessedCount != 10 {
			b.Errorf("Expected 10 processed, got %d", result.ProcessedCount)
		}
	}
}

func BenchmarkWorkerPoolCreation(b *testing.B) {
	mockGitCloner := &MockGitCloner{ShouldFail: false}
	mockDirChecker := &MockDirectoryChecker{ExistingDirs: make(map[string]bool)}
	mockFS := &MockFileSystem{ShouldFailMkdir: false}
	mockEnv := &DefaultEnvironment{}

	config := &Config{
		Workers: 4,
		Quiet:   true,
		RepositoryArgs: []string{"https://github.com/user/repo"},
		Dependencies: &Dependencies{
			FS:       mockFS,
			GitClone: mockGitCloner,
			DirCheck: mockDirChecker,
			Env:      mockEnv,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wp := NewWorkerPool(config)
		_ = wp // Prevent compiler optimization
	}
}