package project

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type Source string

const (
	SourceRemote   Source = "git-remote"
	SourceToplevel Source = "git-toplevel"
	SourceCwd      Source = "cwd"
)

type Resolved struct {
	Slug   string
	Source Source
	Raw    string // pre-slug canonical form, useful for debugging
}

// Resolve walks the fallback chain: git remote → git toplevel → cwd.
// It never returns an empty slug for a non-empty cwd.
func Resolve(cwd string) (Resolved, error) {
	if cwd == "" {
		return Resolved{}, errors.New("cannot resolve project without a working directory")
	}

	if raw, ok := gitRemoteOrigin(cwd); ok {
		canon, err := canonicalRemote(raw)
		if err == nil && canon != "" {
			return Resolved{Slug: Slugify(canon), Source: SourceRemote, Raw: canon}, nil
		}
	}

	if top, ok := gitToplevel(cwd); ok {
		return Resolved{Slug: Slugify(top), Source: SourceToplevel, Raw: top}, nil
	}

	abs, err := filepath.Abs(cwd)
	if err != nil {
		return Resolved{}, fmt.Errorf("could not resolve absolute path for %q: %w", cwd, err)
	}
	return Resolved{Slug: Slugify(abs), Source: SourceCwd, Raw: abs}, nil
}

func gitRemoteOrigin(cwd string) (string, bool) {
	out, err := exec.Command("git", "-C", cwd, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", false
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", false
	}
	return s, true
}

func gitToplevel(cwd string) (string, bool) {
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", false
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", false
	}
	return s, true
}

// canonicalRemote turns a git remote URL into "host/owner/repo" form,
// stripping schemes, user, port, and ".git" suffix.
func canonicalRemote(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("empty remote")
	}
	s = strings.TrimSuffix(s, "/")

	var host, path string

	switch {
	case strings.HasPrefix(s, "ssh://"), strings.HasPrefix(s, "git://"),
		strings.HasPrefix(s, "http://"), strings.HasPrefix(s, "https://"):
		// scheme://[user@]host[:port]/path
		_, rest, _ := strings.Cut(s, "://")
		hostPart, p, ok := strings.Cut(rest, "/")
		if !ok {
			return "", fmt.Errorf("no path in remote: %q", raw)
		}
		path = p
		if at := strings.LastIndex(hostPart, "@"); at >= 0 {
			hostPart = hostPart[at+1:]
		}
		if h, _, ok := strings.Cut(hostPart, ":"); ok {
			hostPart = h
		}
		host = hostPart

	case strings.Contains(s, "@") && strings.Contains(s, ":") && !strings.Contains(s, "://"):
		// scp-like: [user@]host:path
		at := strings.Index(s, "@")
		colon := strings.Index(s, ":")
		if colon < at {
			return "", fmt.Errorf("malformed scp-like remote: %q", raw)
		}
		host = s[at+1 : colon]
		path = s[colon+1:]

	default:
		// bare path-like; treat as cwd-style and slugify the whole thing
		return s, nil
	}

	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	if host == "" || path == "" {
		return "", fmt.Errorf("could not parse remote: %q", raw)
	}
	return host + "/" + path, nil
}
