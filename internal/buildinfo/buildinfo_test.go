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
		// `go build` inside a git checkout stamps a pseudo-version derived from
		// the commit, not a released version. Reporting it would make a source
		// build look newer than the latest release, and `claudeops update`
		// refuses to "downgrade" onto a real release when that happens.
		{
			name:   "pseudo-version from a source build is rejected",
			module: "v0.13.2-0.20260806113558-8abf581c650b",
			want:   defaultVersion,
		},
		{
			name:   "dirty pseudo-version is rejected",
			module: "v0.13.2-0.20260806113558-8abf581c650b+dirty",
			want:   defaultVersion,
		},
		{
			name:   "untagged pseudo-version is rejected",
			module: "v0.0.0-20260806113558-8abf581c650b",
			want:   defaultVersion,
		},
		{
			name:   "build metadata is rejected",
			module: "v2.0.0+incompatible",
			want:   defaultVersion,
		},
		{
			name:   "a real release tag is accepted",
			module: "v0.13.1",
			want:   "0.13.1",
		},
		{
			name:   "a real prerelease tag is accepted",
			module: "v0.14.0-rc.1",
			want:   "0.14.0-rc.1",
		},
		// ldflags come from GoReleaser, which only ever passes a real tag. It is
		// trusted as-is so a deliberate override is never second-guessed.
		{
			name:    "ldflags are trusted even when odd",
			ldflags: "v0.13.2-0.20260806113558-8abf581c650b+dirty",
			want:    "0.13.2-0.20260806113558-8abf581c650b+dirty",
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

func TestIsSourceBuild(t *testing.T) {
	tests := []struct {
		name    string
		ldflags string
		module  string
		want    bool
	}{
		{name: "goreleaser build carries ldflags", ldflags: "0.14.0", module: "", want: false},
		{name: "go install at a tag", ldflags: "", module: "v0.14.0", want: false},
		{name: "go build in a checkout", ldflags: "", module: "v0.14.1-0.20260806113558-8abf581c650b", want: true},
		{name: "dirty checkout", ldflags: "", module: "v0.14.1-0.20260806113558-8abf581c650b+dirty", want: true},
		{name: "devel", ldflags: "", module: "(devel)", want: true},
		{name: "no build info at all", ldflags: "", module: "", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSourceBuild(tt.ldflags, tt.module); got != tt.want {
				t.Fatalf("isSourceBuild(%q, %q) = %v, want %v", tt.ldflags, tt.module, got, tt.want)
			}
		})
	}
}
