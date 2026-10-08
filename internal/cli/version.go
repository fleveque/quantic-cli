package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/fleveque/quantic-cli/internal/render"
)

// Build is the stamp main receives from the linker. Any field can be empty: a
// `go build` or `go install` sets none of them.
type Build struct {
	Version string
	Commit  string
	Date    string // the commit's date, not the build's: see main.go
}

// versionOutput is `quantic version --json`: the first shape in the CLI's
// output contract (design §5). The field names are the contract; the struct
// tags spell them, so renaming a Go field can't rename a JSON one.
type versionOutput struct {
	Schema   string `json:"schema"`
	Version  string `json:"version"`
	Commit   string `json:"commit,omitempty"`
	Date     string `json:"date,omitempty"`
	Go       string `json:"go"`
	Platform string `json:"platform"`
}

const versionSchema = "quantic.cli/version/v1"

func newVersionCmd(opts *Options, build Build) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of quantic",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			info, _ := debug.ReadBuildInfo()
			b := build.withFallback(info)
			out := versionOutput{
				Schema:   versionSchema,
				Version:  b.Version,
				Commit:   b.Commit,
				Date:     b.Date,
				Go:       runtime.Version(),
				Platform: runtime.GOOS + "/" + runtime.GOARCH,
			}

			w := cmd.OutOrStdout()
			if opts.JSON {
				return render.JSON(w, out)
			}
			_, err := fmt.Fprintln(w, out.text())
			return err
		},
	}
}

// withFallback fills what the linker didn't set from what the Go toolchain
// recorded in the binary. `go install …@v0.3.0` records that version. A build
// inside a git checkout records a pseudo-version made of the commit's time and
// hash (v0.0.0-20261008103030-6ff36a315023), with "+dirty" when the tree had
// uncommitted changes, and the commit and its time on their own. info is nil
// when there's nothing recorded.
func (b Build) withFallback(info *debug.BuildInfo) Build {
	if info == nil {
		if b.Version == "" {
			b.Version = "dev"
		}
		return b
	}

	if b.Version == "" {
		b.Version = info.Main.Version
		// "(devel)" is what a build reports when there's no version control
		// to stamp from (a source tarball, -buildvcs=false); "dev" says the
		// same in the form the releases will use.
		if b.Version == "" || b.Version == "(devel)" {
			b.Version = "dev"
		}
	}

	settings := map[string]string{}
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}
	if b.Commit == "" {
		b.Commit = settings["vcs.revision"]
	}
	if b.Date == "" {
		b.Date = settings["vcs.time"]
	}
	return b
}

// text is the line a person reads: as much of the stamp as there is, with
// the commit cut to 12 characters as git does. --json keeps all of it.
func (o versionOutput) text() string {
	commit := o.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}

	s := "quantic " + o.Version
	switch {
	case commit != "" && o.Date != "":
		s += fmt.Sprintf(" (%s, %s)", commit, o.Date)
	case commit != "":
		s += fmt.Sprintf(" (%s)", commit)
	}
	return s + fmt.Sprintf(" %s %s", o.Go, o.Platform)
}
