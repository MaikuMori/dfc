package savedsearch

import (
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	t.Setenv(envRoot, t.TempDir())
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("stale", "#p3 -#later"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("work", "--tag work"); err != nil {
		t.Fatal(err)
	}
	// Reload from disk to confirm persistence.
	s2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if q, ok := s2.Get("stale"); !ok || q != "#p3 -#later" {
		t.Errorf("Get(stale) = %q,%v", q, ok)
	}
	list := s2.List()
	if len(list) != 2 || list[0].Name != "stale" || list[1].Name != "work" {
		t.Errorf("List() = %+v, want sorted [stale work]", list)
	}
	if ok, err := s2.Remove("stale"); err != nil || !ok {
		t.Fatalf("Remove(stale) = %v,%v", ok, err)
	}
	if _, ok := s2.Get("stale"); ok {
		t.Error("stale should be gone after Remove")
	}
	if ok, _ := s2.Remove("nope"); ok {
		t.Error("Remove of missing name should report false")
	}
}

func TestValidName(t *testing.T) {
	for _, bad := range []string{"", "  ", "\t"} {
		if err := ValidName(bad); err == nil {
			t.Errorf("ValidName(%q) should fail", bad)
		}
	}
	// Full-text names (with spaces) are allowed.
	for _, ok := range []string{"good-name", "my important tasks", "Q3 planning"} {
		if err := ValidName(ok); err != nil {
			t.Errorf("ValidName(%q) = %v, want nil", ok, err)
		}
	}
}

func TestSetTrimsAndGetByName(t *testing.T) {
	t.Setenv(envRoot, t.TempDir())
	s, _ := Load()
	if err := s.Set("  my tasks  ", "#p3"); err != nil {
		t.Fatal(err)
	}
	if q, ok := s.Get("my tasks"); !ok || q != "#p3" {
		t.Errorf("Get(\"my tasks\") = %q,%v after trimmed Set", q, ok)
	}
}

func TestSetRejectsEmptyQuery(t *testing.T) {
	t.Setenv(envRoot, t.TempDir())
	s, _ := Load()
	if err := s.Set("x", "   "); err == nil {
		t.Error("empty query should be rejected")
	}
}

func TestSetReloadsBeforeWrite(t *testing.T) {
	t.Setenv(envRoot, t.TempDir())
	s1, _ := Load()
	s2, _ := Load() // both loaded from the same (empty) file

	if err := s1.Set("a", "#a"); err != nil {
		t.Fatal(err)
	}
	// s2 is stale — it doesn't know about "a". Its write must not clobber it.
	if err := s2.Set("b", "#b"); err != nil {
		t.Fatal(err)
	}
	s3, _ := Load()
	if _, ok := s3.Get("a"); !ok {
		t.Error("\"a\" was clobbered by a stale store's write")
	}
	if _, ok := s3.Get("b"); !ok {
		t.Error("\"b\" was not persisted")
	}
}
