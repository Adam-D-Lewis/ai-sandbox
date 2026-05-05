// Package mountresolver turns a project's declarative mount config into
// the final, ordered, deduped, existence-filtered list of host paths used
// for `-v src:src` arguments to docker.
//
// One entry point: Resolve.
package mountresolver

import (
	"os"
	"path/filepath"
	"strings"
)

// Env holds the environment values the resolver substitutes into mount
// strings. Values are pre-resolved by the caller — the resolver never
// reads os.Getenv itself.
type Env struct {
	Home      string
	CWD       string
	SharedDir string
}

// Warner receives one warning per missing mount path. May be nil.
type Warner interface {
	Warn(string)
}

// Resolve expands placeholders ({{HOME}}, {{CWD}}, {{SHARED_DIR}}, ~/, $VAR),
// dedupes each bucket independently, filters via os.Stat, and returns host
// paths ready for `-v src:src`. log may be nil.
//
// Mounts and extras are concatenated in that order. The full mount list is
// expected to come from config; this package does not supply defaults.
func Resolve(mounts, extraMounts []string, env Env, log Warner) []string {
	primary := dedupe(expandAll(mounts, env))
	extras := dedupe(expandAll(extraMounts, env))
	return filterExisting(append(primary, extras...), log)
}

func expandAll(in []string, env Env) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, expand(s, env))
	}
	return out
}

func expand(s string, env Env) string {
	s = strings.ReplaceAll(s, "{{HOME}}", env.Home)
	s = strings.ReplaceAll(s, "{{SHARED_DIR}}", env.SharedDir)
	s = strings.ReplaceAll(s, "{{CWD}}", env.CWD)
	if strings.HasPrefix(s, "~/") {
		s = filepath.Join(env.Home, s[2:])
	} else if s == "~" {
		s = env.Home
	}
	return os.ExpandEnv(s)
}

func dedupe(list []string) []string {
	seen := map[string]bool{}
	out := list[:0]
	for _, m := range list {
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

func filterExisting(paths []string, log Warner) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			if log != nil {
				log.Warn("skip missing mount: " + p)
			}
			continue
		}
		out = append(out, p)
	}
	return out
}
