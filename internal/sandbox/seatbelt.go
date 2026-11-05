// seatbelt.go generates a macOS Seatbelt (sandbox-exec) profile for a run.
//
// Seatbelt is the only real boundary in this package: the allowlist and the
// scrubber are policies the process cooperates with, while the profile is
// enforced by the kernel. It is therefore generated from an explicit policy, not
// from the environment, and it is only used on darwin — everywhere else the
// caller gets an empty profile and runs unconfined.
package sandbox

import (
	"fmt"
	"runtime"
	"strings"
)

// Policy describes what a confined run may do. The zero value denies network and
// write access outside the work directory, which is the safe default for an
// unattended routine.
type Policy struct {
	// WorkDir is the one directory the run may write. Required: a profile with
	// no writable directory would break every tool.
	WorkDir string
	// AllowNetwork permits outbound network access. Off by default: a routine
	// that needs the network should say so.
	AllowNetwork bool
	// ReadHome permits reading the home directory. Off by default, because the
	// home directory is where credentials live.
	ReadHome bool
}

// Profile returns the Seatbelt profile text for p, or "" when confinement is not
// available on this platform.
func Profile(p Policy) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	if strings.TrimSpace(p.WorkDir) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n\n")

	// A confined process still needs to read the system it runs on.
	b.WriteString("(allow process-exec*)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach-lookup)\n")
	b.WriteString("(allow file-read-metadata)\n")
	b.WriteString("(allow file-read* (subpath \"/usr\") (subpath \"/bin\") (subpath \"/sbin\"))\n")
	b.WriteString("(allow file-read* (subpath \"/System\") (subpath \"/Library\"))\n")
	b.WriteString("(allow file-read* (subpath \"/private/var/db/dyld\") (subpath \"/private/tmp\"))\n")

	// The work directory is read-write; that is the point of the run.
	fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %q))\n", p.WorkDir)

	if p.ReadHome {
		b.WriteString("(allow file-read* (subpath \"/Users\"))\n")
	}
	if p.AllowNetwork {
		b.WriteString("(allow network*)\n")
	}
	return b.String()
}

// Command wraps argv so it runs under sandbox-exec with the generated profile.
// It returns argv unchanged when confinement is unavailable, so callers do not
// need a platform branch.
func Command(p Policy, argv []string) []string {
	profile := Profile(p)
	if profile == "" {
		return argv
	}
	// sandbox-exec takes the profile inline with -p; the profile is passed as
	// one argument so its newlines survive.
	return append([]string{"sandbox-exec", "-p", profile}, argv...)
}

// Confined reports whether a policy would actually confine anything here.
func Confined(p Policy) bool {
	return Profile(p) != ""
}
