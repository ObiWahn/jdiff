package theme

import (
	"bytes"
	"os/exec"
	"slices"
	"strings"
)

// Config holds git config entries as printed by git config --get-regexp:
// section and key lowercased, subsections (feature names) as written.
type Config map[string]string

// ReadGitConfig reads the delta and jdiff sections of the effective git
// config, so includes and repository config apply as they do for delta.
// Without git or any matching entries the result is empty.
func ReadGitConfig() Config {
	out, err := exec.Command("git", "config", "--null", "--get-regexp", `^(delta|jdiff)\.`).Output()
	if err != nil {
		return Config{}
	}
	return parseGitConfig(out)
}

// parseGitConfig parses the output of git config --null --get-regexp: one
// "key\nvalue" record per NUL. Later entries win, as in git.
func parseGitConfig(out []byte) Config {
	cfg := Config{}
	for rec := range bytes.SplitSeq(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		key, value, _ := strings.Cut(string(rec), "\n")
		cfg[key] = value
	}
	return cfg
}

// Features returns the enabled delta features, highest priority first,
// following delta's rules: features from DELTA_FEATURES (env) and --features
// (flag) come first; [delta] features is used only when neither replaces it
// (a leading "+" in env adds instead). Each feature precedes the features it
// enables itself, and among the features of one setting the rightmost wins.
func Features(cfg Config, env, flag string) []string {
	var input []string
	useConfig := flag == ""
	switch {
	case strings.HasPrefix(env, "+"):
		input = append(strings.Fields(env[1:]), strings.Fields(flag)...)
	case env != "":
		input = strings.Fields(env)
		useConfig = false
	default:
		input = strings.Fields(flag)
	}

	var out []string
	var visit func(f string)
	visit = func(f string) {
		out = append(out, f)
		children := strings.Fields(cfg["delta."+f+".features"])
		for _, c := range slices.Backward(children) {
			if !slices.Contains(out, c) {
				visit(c)
			}
		}
	}
	for _, f := range slices.Backward(input) {
		visit(f)
	}
	if useConfig {
		for _, f := range slices.Backward(strings.Fields(cfg["delta.features"])) {
			visit(f)
		}
	}
	return out
}

// lookup returns the value of a delta option: the main [delta] section wins,
// then the features in priority order.
func lookup(cfg Config, features []string, key string) (string, bool) {
	if v, ok := cfg["delta."+key]; ok {
		return v, true
	}
	for _, f := range features {
		if v, ok := cfg["delta."+f+"."+key]; ok {
			return v, true
		}
	}
	return "", false
}
