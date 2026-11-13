# 📁 gclone

> **🚀 Smart Git Repository Cloner** - Clone repositories into organized directory structures with parallel processing

[![Go Version](https://img.shields.io/badge/go-%3E%3D1.22-00ADD8?style=flat-square&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg?style=flat-square)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/juev/gclone?style=flat-square)](https://goreportcard.com/report/github.com/juev/gclone)

## ✨ Why gclone?

Ever tired of manually creating nested directories for your Git repositories? **gclone** automatically organizes your repos into a clean, predictable structure that mirrors the repository's URL path, **with blazing-fast parallel processing**.

```text
~/src/
├── github.com/
│   ├── golang/go/
│   ├── kubernetes/kubernetes/
│   └── juev/gclone/
├── gitlab.com/
│   └── company/project/
└── bitbucket.org/
    └── team/service/
```

## 🚀 Key Features

- **⚡ Parallel Processing** - Clone multiple repositories simultaneously with configurable workers
- **🎯 Zero Configuration** - Works out of the box with sensible defaults  
- **📂 Organized Structure** - Mirrors repository URLs in your filesystem
- **🔒 Security First** - Comprehensive URL validation and path sanitization
- **💾 Smart Caching** - Intelligent directory existence checking with LRU cache
- **🌊 Shallow Clones** - Optional `--depth=1` for faster clones
- **🛡️ Graceful Shutdown** - Handles interrupts cleanly with signal handling
- **🤫 Quiet Mode** - Suppress output when needed
- **🐚 Shell Integration** - Perfect for scripts and aliases

## 📦 Installation

```bash
go install github.com/juev/gclone@latest
```

## 🔧 Quick Start

### Basic Usage

```bash
# Set your preferred projects directory
export GIT_PROJECT_DIR="$HOME/src"

# Clone a single repository
gclone https://github.com/golang/go.git
# Result: ~/src/github.com/golang/go/

# Clone multiple repositories in parallel
gclone https://github.com/golang/go.git \
       https://github.com/kubernetes/kubernetes.git \
       https://gitlab.com/company/project.git

# Fast shallow clone
gclone --shallow https://github.com/large/repository.git

# SSH and scp-like forms work the same way
gclone git@ssh.bitbucket.org:team/service.git
# Result: ~/src/bitbucket.org/team/service/
```

The host of the URL becomes the first directory. It is lowercased, and a leading `ssh` label is
dropped, because that label marks a forge's SSH endpoint and not a different service:
`git@ssh.bitbucket.org:team/service.git`, `ssh://git@ssh.bitbucket.org:443/team/service.git` and
`git@bitbucket.org:team/service.git` all clone into `$GIT_PROJECT_DIR/bitbucket.org/team/service`.
The same holds for `ssh.github.com`, GitHub's SSH endpoint on port 443. Every other host keeps its
name as written: `git@git.example.com:team/service.git` still clones into
`$GIT_PROJECT_DIR/git.example.com/team/service`. Pass `--full-host` to keep the `ssh` label, for
the rare host where `ssh.<domain>` serves a different repository than `<domain>`.

### Advanced Usage

```bash
# Use 8 parallel workers for faster cloning
gclone -w 8 \
  https://github.com/repo1/project.git \
  https://github.com/repo2/project.git \
  https://github.com/repo3/project.git

# Quiet mode for scripts
gclone --quiet --shallow https://github.com/user/repo.git

# Chain with other commands
cd "$(gclone https://github.com/user/repo.git)"
code "$(gclone --quiet https://github.com/user/repo.git)"
```

## 💡 Command Line Options

```bash
gclone [-h] [-v] [-q] [-s] [--full-host] [-w WORKERS] [REPOSITORY...]

Options:
  -h, --help         Show this help message and exit
  -v, --version      Show the version number and exit
  -q, --quiet        Suppress output
  -s, --shallow      Perform shallow clone with --depth=1
      --full-host    Keep a leading ssh. hostname label in the project path
  -w, --workers      Number of parallel workers (default: 4, max: 32)

Environment Variables:
  GIT_PROJECT_DIR    Directory to clone repositories (default: current dir)

Examples:
  GIT_PROJECT_DIR="$HOME/src" gclone https://github.com/user/repo
  gclone -w 8 https://github.com/user/repo1 https://github.com/user/repo2
```

## 🎨 Shell Integration

### Bash/Zsh Functions

```bash
# Clone and cd into repository
gcd() {
    cd "$(gclone "$1")"
}

# Clone and open in VS Code
gcode() {
    code "$(gclone "$1")"
}

# Clone and open in your editor
gedit() {
    $EDITOR "$(gclone "$1")"
}

# Bulk clone with parallel processing
gbulk() {
    gclone -w 8 "$@"
}
```

### Fish Shell Functions

```fish
function gcd --argument repo
    test -n "$repo"; or return 1
    cd (gclone $repo)
end

function gcode --argument repo
    test -n "$repo"; or return 1
    code (gclone $repo)
end

function gbulk
    gclone -w 8 $argv
end
```

### PowerShell Functions

```powershell
function gcd($repo) {
    Set-Location (gclone $repo)
}

function gcode($repo) {
    code (gclone $repo)
}

function gbulk {
    gclone -w 8 @args
}
```

## 🏗️ How It Works

1. **Parse & Validate** - Security validation of repository URLs
2. **Normalize** - Convert URLs to filesystem paths (with caching)
3. **Parallel Process** - Distribute work across configurable workers
4. **Smart Check** - Cache-optimized directory existence checking
5. **Secure Clone** - Timeout-protected git operations
6. **Signal Handling** - Graceful shutdown on interrupts

## 🔒 Security Features

- **URL Validation** - Comprehensive security checks against malicious URLs
- **Path Sanitization** - Protection against directory traversal attacks
- **Command Safety** - No shell interpretation, direct git execution
- **Timeout Protection** - 10-minute timeout per repository
- **Safe Defaults** - Conservative permissions (0750) for created directories

## ⚡ Performance Features

- **Parallel Workers** - Configurable concurrent cloning (1-32 workers)
- **Smart Caching** - LRU cache for directory existence checks
- **Sequential Fallback** - Automatic optimization for single repositories
- **Memory Efficient** - Streaming directory reads, minimal allocations

## 🎯 Use Cases

- **📚 Learning** - Quickly clone course materials and tutorials
- **🔧 Development** - Maintain consistent project structure across teams
- **🤖 CI/CD** - Bulk repository setup in automation pipelines
- **🔍 Research** - Rapidly explore multiple open source projects
- **📋 Backup** - Mirror repositories locally with organized structure
- **🏢 Enterprise** - Standardized repository organization

## 📊 Status & Monitoring

The tool provides detailed feedback during operation:

```bash
# Progress indicators for multiple repositories
gclone repo1.git repo2.git repo3.git
# processed 3 repositories: 2 successful, 1 failed

# Existing repositories are detected
gclone https://github.com/existing/repo.git
# repository already exists: /home/user/src/github.com/existing/repo

# Graceful interrupt handling
^C Received signal interrupt, initiating graceful shutdown...
Graceful shutdown completed.
```

## 🤝 Contributing

Contributions are welcome! The codebase features:

- **Dependency Injection** - Clean interfaces for testability
- **Comprehensive Testing** - Full test coverage with mocks
- **Security Focus** - Multiple layers of validation
- **Performance Optimization** - Caching and parallelization

## 📄 License

MIT License - see [LICENSE](LICENSE) file for details.

---

Made with ❤️ by developers, for developers

⭐ **Star this repo if it helps you stay organized and productive!**
