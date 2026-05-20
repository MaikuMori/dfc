package storage

import (
	"strings"
	"testing"
	"time"
)

func TestTaskRoundtrip(t *testing.T) {
	now := time.Date(2026, 5, 13, 14, 22, 1, 0, time.UTC)
	in := Task{
		ID:          "01J9X7K3M8VQNH4Z7Y3PG2T5BD",
		Status:      StatusOpen,
		Created:     now,
		Description: "Buy milk",
		Details:     "Optional extra notes\nspanning multiple lines.",
	}
	b, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Unmarshal(b)
	if err != nil {
		t.Fatalf("unmarshal: %v\n---\n%s", err, b)
	}
	if out.ID != in.ID || out.Status != in.Status {
		t.Errorf("meta mismatch: %+v", out)
	}
	if !out.Created.Equal(in.Created) {
		t.Errorf("created = %v, want %v", out.Created, in.Created)
	}
	if out.Description != in.Description {
		t.Errorf("description = %q, want %q", out.Description, in.Description)
	}
	if out.Details != in.Details {
		t.Errorf("details = %q, want %q", out.Details, in.Details)
	}
}

func TestTaskNoDetails(t *testing.T) {
	in := Task{
		ID:          "ID1",
		Status:      StatusDone,
		Created:     time.Now().UTC().Truncate(time.Second),
		Description: "just this",
	}
	b, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\n# just this\n") {
		t.Errorf("missing `# desc` line in output:\n%s", b)
	}
	if !strings.Contains(string(b), "---\n\n# ") {
		t.Errorf("missing blank line after frontmatter:\n%s", b)
	}
	out, err := Unmarshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.Description != "just this" || out.Details != "" {
		t.Errorf("got desc=%q details=%q", out.Description, out.Details)
	}
}

func TestUnmarshalFallsBackToFirstTextLine(t *testing.T) {
	raw := []byte("---\nid: x\nstatus: open\ncreated: 2026-05-13T00:00:00Z\n---\n\nplain description\n\ndetails follow\n")
	out, err := Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.Description != "plain description" {
		t.Errorf("description = %q", out.Description)
	}
	if out.Details != "details follow" {
		t.Errorf("details = %q", out.Details)
	}
}

func TestUnmarshalStripsHeadingPrefixes(t *testing.T) {
	raw := []byte("---\nid: x\nstatus: open\ncreated: 2026-05-13T00:00:00Z\n---\n\n## fancy heading\n\ndetails\n")
	out, err := Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.Description != "fancy heading" {
		t.Errorf("description = %q", out.Description)
	}
	if out.Details != "details" {
		t.Errorf("details = %q", out.Details)
	}
}

func TestUnmarshalPrefersH1OverH2(t *testing.T) {
	raw := []byte("---\nid: x\nstatus: open\ncreated: 2026-05-13T00:00:00Z\n---\n\n## first\n\n# second\n\ndetails\n")
	out, err := Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.Description != "second" {
		t.Errorf("description = %q, want %q", out.Description, "second")
	}
	if out.Details != "details" {
		t.Errorf("details = %q", out.Details)
	}
}

func TestUnmarshalH2WhenNoH1(t *testing.T) {
	raw := []byte("---\nid: x\nstatus: open\ncreated: 2026-05-13T00:00:00Z\n---\n\n## only\n\ndetails\n")
	out, err := Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.Description != "only" {
		t.Errorf("description = %q", out.Description)
	}
	if out.Details != "details" {
		t.Errorf("details = %q", out.Details)
	}
}

func TestUnmarshalSkipsHashInsideCodeFence(t *testing.T) {
	raw := []byte("---\nid: x\nstatus: open\ncreated: 2026-05-13T00:00:00Z\n---\n\n```\n# not a heading\n```\n\n# real heading\n\ndetails\n")
	out, err := Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.Description != "real heading" {
		t.Errorf("description = %q, want %q", out.Description, "real heading")
	}
}

func TestUnmarshalSetextHeading(t *testing.T) {
	raw := []byte("---\nid: x\nstatus: open\ncreated: 2026-05-13T00:00:00Z\n---\n\nReal heading\n============\n\ndetails\n")
	out, err := Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out.Description != "Real heading" {
		t.Errorf("description = %q", out.Description)
	}
	if out.Details != "details" {
		t.Errorf("details = %q", out.Details)
	}
}

func TestUnmarshalRejectsMissingFrontmatter(t *testing.T) {
	if _, err := Unmarshal([]byte("no frontmatter here\n")); err == nil {
		t.Fatal("expected error")
	}
}

func TestUnmarshalRejectsUnterminatedFrontmatter(t *testing.T) {
	if _, err := Unmarshal([]byte("---\nid: x\nstatus: open\n")); err == nil {
		t.Fatal("expected error")
	}
}
