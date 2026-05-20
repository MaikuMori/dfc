package ui

import (
	"errors"
	"testing"
)

func TestEditorArgvErrorsWithoutEnv(t *testing.T) {
	t.Setenv("DFC_EDITOR", "")
	t.Setenv("EDITOR", "")
	if _, err := editorArgv("/tmp/file.md"); !errors.Is(err, errNoEditor) {
		t.Errorf("expected errNoEditor, got %v", err)
	}
}

func TestEditorArgvDFCEditorBeatsEditor(t *testing.T) {
	t.Setenv("DFC_EDITOR", "code --wait")
	t.Setenv("EDITOR", "vi")
	got, err := editorArgv("/tmp/file.md")
	if err != nil {
		t.Fatalf("editorArgv: %v", err)
	}
	want := []string{"code", "--wait", "/tmp/file.md"}
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("argv[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestEditorArgvFallsThroughToEditor(t *testing.T) {
	t.Setenv("DFC_EDITOR", "")
	t.Setenv("EDITOR", "nano")
	got, err := editorArgv("/tmp/file.md")
	if err != nil {
		t.Fatalf("editorArgv: %v", err)
	}
	if got[0] != "nano" || got[len(got)-1] != "/tmp/file.md" {
		t.Errorf("EDITOR fallback wrong; got %v", got)
	}
}

func TestEditorArgvPassesThroughExtraArgs(t *testing.T) {
	t.Setenv("DFC_EDITOR", "vim -u NONE +startinsert")
	got, err := editorArgv("/tmp/file.md")
	if err != nil {
		t.Fatalf("editorArgv: %v", err)
	}
	want := []string{"vim", "-u", "NONE", "+startinsert", "/tmp/file.md"}
	if len(got) != len(want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("argv[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestEditorArgvWhitespaceOnlyTreatedAsUnset(t *testing.T) {
	t.Setenv("DFC_EDITOR", "   \t ")
	t.Setenv("EDITOR", "")
	if _, err := editorArgv("/tmp/file.md"); !errors.Is(err, errNoEditor) {
		t.Errorf("whitespace-only DFC_EDITOR should fall through to errNoEditor, got %v", err)
	}
}
