package buildinfo

import "testing"

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name    string
		ldflags string
		module  string
		want    string
	}{
		{
			name:    "ldflags wins",
			ldflags: "v0.14.0",
			module:  "v0.13.1",
			want:    "0.14.0",
		},
		{
			name:    "ldflags leading v stripped",
			ldflags: "v1.2.3",
			want:    "1.2.3",
		},
		{
			name:    "ldflags without v kept",
			ldflags: "1.2.3",
			want:    "1.2.3",
		},
		{
			name:   "module version when no ldflags",
			module: "v0.13.1",
			want:   "0.13.1",
		},
		{
			name:   "devel module falls back to default",
			module: "(devel)",
			want:   defaultVersion,
		},
		{
			name: "nothing known falls back to default",
			want: defaultVersion,
		},
		{
			name:    "whitespace is trimmed",
			ldflags: "  v0.9.0\n",
			want:    "0.9.0",
		},
		{
			name:    "goreleaser snapshot suffix is preserved",
			ldflags: "v0.14.0-next",
			want:    "0.14.0-next",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.ldflags, tt.module); got != tt.want {
				t.Fatalf("resolveVersion(%q, %q) = %q, want %q", tt.ldflags, tt.module, got, tt.want)
			}
		})
	}
}

func TestResolveCommit(t *testing.T) {
	tests := []struct {
		name    string
		ldflags string
		vcs     string
		want    string
	}{
		{name: "ldflags wins", ldflags: "abc1234", vcs: "def5678901234", want: "abc1234"},
		{name: "vcs revision is shortened", vcs: "def567890123456789", want: "def5678"},
		{name: "short vcs revision kept whole", vcs: "def", want: "def"},
		{name: "unknown", want: unknownValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveCommit(tt.ldflags, tt.vcs); got != tt.want {
				t.Fatalf("resolveCommit(%q, %q) = %q, want %q", tt.ldflags, tt.vcs, got, tt.want)
			}
		})
	}
}

func TestResolveDate(t *testing.T) {
	tests := []struct {
		name    string
		ldflags string
		vcs     string
		want    string
	}{
		{name: "ldflags wins", ldflags: "2026-08-06T11:00:00Z", vcs: "2020-01-01T00:00:00Z", want: "2026-08-06T11:00:00Z"},
		{name: "vcs time used", vcs: "2020-01-01T00:00:00Z", want: "2020-01-01T00:00:00Z"},
		{name: "unknown", want: unknownValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveDate(tt.ldflags, tt.vcs); got != tt.want {
				t.Fatalf("resolveDate(%q, %q) = %q, want %q", tt.ldflags, tt.vcs, got, tt.want)
			}
		})
	}
}

// The default version is what a plain `go build` of this source tree reports,
// and what the release workflow asserts the pushed tag against. An empty or
// "v"-prefixed value would break both.
func TestDefaultVersionIsUsableSemver(t *testing.T) {
	if defaultVersion == "" {
		t.Fatal("defaultVersion must not be empty")
	}
	if defaultVersion[0] == 'v' {
		t.Fatalf("defaultVersion %q must not carry a leading v", defaultVersion)
	}
}

func TestExportedAccessorsNeverReturnEmpty(t *testing.T) {
	if Version() == "" {
		t.Fatal("Version() must not be empty")
	}
	if Commit() == "" {
		t.Fatal("Commit() must not be empty")
	}
	if Date() == "" {
		t.Fatal("Date() must not be empty")
	}
}
