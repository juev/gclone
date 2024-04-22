package main

import (
	"path/filepath"
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
			want:          "src/host.xz/~user/path/to/repo.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", tt.homeVar)
			t.Setenv("GIT_PROJECT_DIR", tt.gitProjectDir)

			if got := getProjectDir(tt.repository); got != tt.want {
				t.Errorf("parseEnvs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isNotEmpty(t *testing.T) {
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
			got := isNotEmpty(filepath.Join("testdata", tt.args.name))
			if got != tt.want {
				t.Errorf("isEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parse(t *testing.T) {
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
			wantRepo: "host.xz/~user/path/to/repo.git",
		},
		{
			name: "git",
			args: args{
				repository: "git@git.sr.ht:~libreboot/lbmk",
			},
			wantRepo: "git.sr.ht/~libreboot/lbmk",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRepo := parse(tt.args.repository)
			if gotRepo != tt.wantRepo {
				t.Errorf("parse() gotRepo = %v, want %v", gotRepo, tt.wantRepo)
			}
		})
	}
}
