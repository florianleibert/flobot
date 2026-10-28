package server

import (
	"os"
	"path/filepath"
	"strings"
)

// flocode refuses to start in a handful of directories (its own workspace
// safety gate): the home directory, the filesystem root, a temporary
// directory, a system directory, or anything inside a .git folder.
//
// flobot spawns flocode with cwd = the workspace, so a workspace flocode
// would refuse makes EVERY run fail with a message that reads like a flobot
// bug ("CRITICAL SAFETY BLOCK: You are attempting to run FloCode in a
// temporary directory"). The same rules are enforced here, at selection
// time, so the user gets one clear sentence instead of a mystery at spawn.
var systemWorkspaceDirs = []string{
	"/etc",
	"/var",
	"/usr",
	"/bin",
	"/sbin",
	"/lib",
	"/System",
	"/Library",
	"/Applications",
}

// WorkspaceRefusalReason returns why abs cannot be used as a workspace, or
// "" when it can. It mirrors flocode's safety gate so the two agree.
func WorkspaceRefusalReason(abs string) string {
	if strings.TrimSpace(abs) == "" {
		return "no workspace path given"
	}
	abs = filepath.Clean(abs)
	if home, err := os.UserHomeDir(); err == nil && abs == filepath.Clean(home) {
		return "that is your home directory — pick a project folder (e.g. ~/dev/my-project)"
	}
	if abs == "/" || abs == filepath.VolumeName(abs)+string(filepath.Separator) {
		return "that is the filesystem root — pick a project folder"
	}
	for _, td := range tempWorkspaceDirs() {
		if sameWorkspaceDir(abs, td) {
			return "that is a temporary directory (" + td + ") — flocode refuses to run there; pick a project folder"
		}
	}
	for _, sd := range systemWorkspaceDirs {
		if sameWorkspaceDir(abs, sd) {
			return "that is a system directory (" + sd + ") — flocode refuses to run there; pick a project folder"
		}
	}
	for _, part := range strings.Split(abs, string(filepath.Separator)) {
		if strings.EqualFold(part, ".git") {
			return "that is inside a .git folder — pick the repository root"
		}
	}
	return ""
}

// tempWorkspaceDirs lists the directories flocode treats as temporary.
func tempWorkspaceDirs() []string {
	dirs := []string{"/tmp", "/var/tmp", "/private/tmp"}
	if td := os.TempDir(); td != "" {
		dirs = append(dirs, td)
	}
	return dirs
}

// sameWorkspaceDir compares two paths, resolving symlinks when it can:
// macOS maps /var to /private/var and /tmp to /private/tmp.
func sameWorkspaceDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false
	}
	return ra == rb
}
