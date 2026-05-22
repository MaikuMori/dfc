package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/MaikuMori/dfc/internal/project"
	"github.com/MaikuMori/dfc/internal/storage"
	"github.com/MaikuMori/dfc/internal/trash"
	"github.com/posener/complete"
)

// EnvCompleteFormat selects how predictor output is rendered. The default
// "plain" emits one candidate per line (bash-safe via posener/complete).
// "rich" emits `id<TAB>description` for shells that can split on a tab
// (native zsh `_describe`, fish `complete -a`).
const EnvCompleteFormat = "DFC_COMPLETE_FORMAT"

const completeDescMaxLen = 60

// Predictors returns the kongplete predictor map. Pass into
// kongplete.WithPredictors when wiring kongplete.Complete.
func Predictors() map[string]complete.Predictor {
	return map[string]complete.Predictor{
		"project":   complete.PredictFunc(predictProjects),
		"tag":       complete.PredictFunc(predictTags),
		"task-open": complete.PredictFunc(predictTasksFn(scopeTaskOpen)),
		"task-done": complete.PredictFunc(predictTasksFn(scopeTaskDone)),
		"task-any":  complete.PredictFunc(predictTasksFn(scopeTaskAny)),
		"trash-id":  complete.PredictFunc(predictTrashIDs),
	}
}

func predictTags(_ complete.Args) []string {
	reg, err := project.LoadRegistry()
	if err != nil {
		return nil
	}
	all := reg.AllTags()
	rich := richFormat()
	out := make([]string, 0, len(all)+1)
	for _, t := range all {
		if rich {
			out = append(out, fmt.Sprintf("%s\t%d project%s", t.Name, len(t.Slugs), pluralS(len(t.Slugs))))
			continue
		}
		out = append(out, t.Name)
	}
	if len(reg.UntaggedSlugs()) > 0 {
		if rich {
			out = append(out, fmt.Sprintf("(untagged)\t%d project%s", len(reg.UntaggedSlugs()), pluralS(len(reg.UntaggedSlugs()))))
		} else {
			out = append(out, "(untagged)")
		}
	}
	return out
}

// taskFilter narrows the predictor's view onto a status subset.
type taskFilter int

const (
	scopeTaskAny taskFilter = iota
	scopeTaskOpen
	scopeTaskDone
)

func (f taskFilter) keep(t storage.Task) bool {
	switch f {
	case scopeTaskOpen:
		return t.Status == storage.StatusOpen
	case scopeTaskDone:
		return t.Status == storage.StatusDone
	default:
		return true
	}
}

// projectScope describes which projects a task predictor should read.
// When AllProjects is true, every registered project contributes;
// otherwise Slug is the single project being completed against.
type projectScope struct {
	AllProjects bool
	Slug        string
}

// scopeFromArgs reads --all-projects / --all / -a and -p / --project from
// the already-completed portion of the command line. When neither
// appears, the cwd-resolved slug wins. -p values are run through the
// registry so user-typed display names route to the canonical slug.
func scopeFromArgs(a complete.Args) projectScope {
	var rawScope string
	for i, w := range a.Completed {
		switch w {
		case "-a", "--all-projects", "--all":
			return projectScope{AllProjects: true}
		case "-p", "--project":
			if i+1 < len(a.Completed) {
				rawScope = a.Completed[i+1]
			}
		}
		if v, ok := strings.CutPrefix(w, "--project="); ok {
			rawScope = v
		}
		if v, ok := strings.CutPrefix(w, "-p="); ok {
			rawScope = v
		}
	}
	if rawScope != "" {
		if reg, err := project.LoadRegistry(); err == nil {
			if slug, _ := reg.LookupSlug(rawScope); slug != "" {
				return projectScope{Slug: slug}
			}
		}
		return projectScope{Slug: rawScope}
	}
	slug, err := resolveCwdSlug()
	if err != nil {
		return projectScope{}
	}
	return projectScope{Slug: slug}
}

func predictProjects(_ complete.Args) []string {
	reg, err := project.LoadRegistry()
	if err != nil {
		return nil
	}
	slugs := reg.Slugs()
	sort.SliceStable(slugs, func(i, j int) bool {
		li, lj := reg.LastUsed(slugs[i]), reg.LastUsed(slugs[j])
		if li.Equal(lj) {
			return slugs[i] < slugs[j]
		}
		return li.After(lj)
	})
	rich := richFormat()
	out := make([]string, 0, len(slugs))
	for _, s := range slugs {
		candidate, alternate := projectCandidate(reg.Name(s), s)
		if rich && alternate != "" {
			out = append(out, candidate+"\t"+truncate(alternate))
			continue
		}
		out = append(out, candidate)
	}
	return out
}

// projectCandidate picks what to put on the completion line for a
// project. Names are friendlier than slugs but only some are shell-
// safe; when the name has no characters that would force the user to
// quote, we surface the name as the candidate and use the slug as the
// rich-format description. Otherwise the slug is the candidate (always
// shell-safe) and the name becomes the description.
func projectCandidate(name, slug string) (candidate, alternate string) {
	if name != "" && name != slug && isShellSafe(name) {
		return name, slug
	}
	if name != "" && name != slug {
		return slug, name
	}
	return slug, ""
}

// isShellSafe reports whether s contains only characters that don't
// require quoting in bash / zsh / fish. We're conservative on purpose;
// the slug is always a safe fallback.
func isShellSafe(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == '/':
		default:
			return false
		}
	}
	return true
}

func predictTasksFn(filter taskFilter) func(complete.Args) []string {
	return func(a complete.Args) []string {
		sc := scopeFromArgs(a)
		tasks := listTasksScoped(sc)
		rich := richFormat()
		out := make([]string, 0, len(tasks))
		for _, t := range tasks {
			if !filter.keep(t) {
				continue
			}
			if rich {
				out = append(out, t.ID+"\t"+truncate(t.Description))
				continue
			}
			out = append(out, t.ID)
		}
		return out
	}
}

func predictTrashIDs(_ complete.Args) []string {
	entries, err := trash.List()
	if err != nil {
		return nil
	}
	rich := richFormat()
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if rich {
			label := e.Description
			if label == "" {
				label = string(e.Kind) + " " + e.Slug
			}
			out = append(out, e.ID+"\t"+truncate(label))
			continue
		}
		out = append(out, e.ID)
	}
	return out
}

// listTasksScoped reads tasks across the projects implied by sc. Missing
// project directories are skipped silently — completion should never
// surface filesystem errors.
func listTasksScoped(sc projectScope) []storage.Task {
	var slugs []string
	if sc.AllProjects {
		reg, err := project.LoadRegistry()
		if err != nil {
			return nil
		}
		slugs = reg.Slugs()
	} else if sc.Slug != "" {
		slugs = []string{sc.Slug}
	}
	var all []storage.Task
	for _, slug := range slugs {
		if !storage.ProjectDirExists(slug) {
			continue
		}
		store, err := storage.Open(slug)
		if err != nil {
			continue
		}
		ts, err := store.List()
		if err != nil {
			continue
		}
		all = append(all, ts...)
	}
	return all
}

func richFormat() bool {
	return strings.ToLower(strings.TrimSpace(os.Getenv(EnvCompleteFormat))) == "rich"
}

// truncate caps s at max runes, replacing the tail with "…" when it
// overflows. Rune-aware so multibyte glyphs don't blow the visual
// budget that completion menus enforce.
func truncate(s string) string {
	rs := []rune(s)
	if len(rs) <= completeDescMaxLen {
		return s
	}
	return string(rs[:completeDescMaxLen-1]) + "…"
}
