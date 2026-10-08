package cli

import (
	"runtime/debug"
	"testing"
)

func TestBuildWithFallback(t *testing.T) {
	checkout := &debug.BuildInfo{
		Main: debug.Module{Version: "v0.0.0-20261008103030-6ff36a315023+dirty"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "6ff36a315023f813afce2a7501e35341f6f773c5"},
			{Key: "vcs.time", Value: "2026-10-08T10:30:30Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}

	tests := []struct {
		name  string
		build Build
		info  *debug.BuildInfo
		want  Build
	}{
		{
			name: "nothing recorded",
			info: nil,
			want: Build{Version: "dev"},
		},
		{
			name: "no version control",
			info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			want: Build{Version: "dev"},
		},
		{
			name: "go install of a tag",
			info: &debug.BuildInfo{Main: debug.Module{Version: "v0.3.0"}},
			want: Build{Version: "v0.3.0"},
		},
		{
			name: "build in a checkout",
			info: checkout,
			want: Build{
				Version: "v0.0.0-20261008103030-6ff36a315023+dirty",
				Commit:  "6ff36a315023f813afce2a7501e35341f6f773c5",
				Date:    "2026-10-08T10:30:30Z",
			},
		},
		{
			// A release sets all three; nothing the toolchain recorded may
			// replace them.
			name:  "linker stamp wins",
			build: Build{Version: "v0.1.0", Commit: "abc1234", Date: "2026-10-01T00:00:00Z"},
			info:  checkout,
			want:  Build{Version: "v0.1.0", Commit: "abc1234", Date: "2026-10-01T00:00:00Z"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build.withFallback(tt.info); got != tt.want {
				t.Errorf("withFallback() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestVersionText(t *testing.T) {
	tests := []struct {
		name string
		out  versionOutput
		want string
	}{
		{
			name: "everything",
			out:  versionOutput{Version: "v0.1.0", Commit: "6ff36a315023f813afce", Date: "2026-10-08T10:30:30Z", Go: "go1.27.1", Platform: "linux/amd64"},
			want: "quantic v0.1.0 (6ff36a315023, 2026-10-08T10:30:30Z) go1.27.1 linux/amd64",
		},
		{
			name: "no date",
			out:  versionOutput{Version: "v0.1.0", Commit: "abc1234", Go: "go1.27.1", Platform: "darwin/arm64"},
			want: "quantic v0.1.0 (abc1234) go1.27.1 darwin/arm64",
		},
		{
			name: "nothing but the version",
			out:  versionOutput{Version: "dev", Go: "go1.27.1", Platform: "linux/amd64"},
			want: "quantic dev go1.27.1 linux/amd64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.out.text(); got != tt.want {
				t.Errorf("text() = %q, want %q", got, tt.want)
			}
		})
	}
}
