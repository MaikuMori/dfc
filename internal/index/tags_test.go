package index

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/MaikuMori/dfc/internal/storage"
)

// tagRows returns every task_tags row as "id:tag", sorted.
func tagRows(t *testing.T, idx *Index) []string {
	t.Helper()
	rows, err := idx.db.Query(`SELECT id, tag FROM task_tags`)
	if err != nil {
		t.Fatalf("task_tags query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id, tg string
		if err := rows.Scan(&id, &tg); err != nil {
			t.Fatal(err)
		}
		out = append(out, id+":"+tg)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func TestUpsertMirrorsTags(t *testing.T) {
	idx := openTempIndex(t)
	task := mkTask("01TAGAAAAAAAAAAAAAAAAAAAA1", "ship it #P3", "body #journal/2024")
	if err := idx.Upsert(task); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"01TAGAAAAAAAAAAAAAAAAAAAA1:journal/2024",
		"01TAGAAAAAAAAAAAAAAAAAAAA1:p3", // stored lowercased
	}
	if got := tagRows(t, idx); !slices.Equal(got, want) {
		t.Fatalf("tag rows after upsert = %v, want %v", got, want)
	}

	// Re-upserting with different content replaces the mirror, not appends.
	task.Description = "ship it #later"
	task.Details = ""
	if err := idx.Upsert(task); err != nil {
		t.Fatal(err)
	}
	want = []string{"01TAGAAAAAAAAAAAAAAAAAAAA1:later"}
	if got := tagRows(t, idx); !slices.Equal(got, want) {
		t.Fatalf("tag rows after re-upsert = %v, want %v", got, want)
	}

	// Deleting the task cascades to its tag rows via the trigger.
	if err := idx.Delete(task.ID); err != nil {
		t.Fatal(err)
	}
	if got := tagRows(t, idx); got != nil {
		t.Fatalf("tag rows after delete = %v, want none", got)
	}
}

func TestSearchTagFilters(t *testing.T) {
	idx := openTempIndex(t)
	mk := func(id, slug, desc string) {
		task := mkTask(id, desc, "")
		task.ProjectSlug = slug
		if err := idx.Upsert(task); err != nil {
			t.Fatal(err)
		}
	}
	mk("01TAGBBBBBBBBBBBBBBBBBBBB1", "acme", "alpha one #p3")
	mk("01TAGBBBBBBBBBBBBBBBBBBBB2", "acme", "alpha two #later")
	mk("01TAGBBBBBBBBBBBBBBBBBBBB3", "acme", "alpha three #journal/2024")
	mk("01TAGBBBBBBBBBBBBBBBBBBBB4", "beta", "alpha four untagged")

	search := func(filters ...TagFilter) []string {
		hits, err := idx.Search("alpha", SearchOpts{Status: "all", Limit: 50, Tags: filters})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, h := range hits {
			out = append(out, h.Task.Description)
		}
		slices.Sort(out)
		return out
	}

	cases := []struct {
		name   string
		filter TagFilter
		want   []string
	}{
		{"include", TagFilter{Tag: "p3"}, []string{"alpha one #p3"}},
		{"include is case-insensitive", TagFilter{Tag: "P3"}, []string{"alpha one #p3"}},
		{"hierarchy", TagFilter{Tag: "journal"}, []string{"alpha three #journal/2024"}},
		{"no bare-prefix false positive", TagFilter{Tag: "jour"}, nil},
		{
			"exclude",
			TagFilter{Tag: "later", Exclude: true},
			[]string{"alpha four untagged", "alpha one #p3", "alpha three #journal/2024"},
		},
		{
			"project allowlist matches by slug",
			TagFilter{Tag: "work", Projects: []string{"beta"}},
			[]string{"alpha four untagged"},
		},
		{
			"exclude with project allowlist",
			TagFilter{Tag: "work", Projects: []string{"beta"}, Exclude: true},
			[]string{"alpha one #p3", "alpha three #journal/2024", "alpha two #later"},
		},
	}
	for _, c := range cases {
		if got := search(c.filter); !slices.Equal(got, c.want) {
			t.Errorf("%s: Search = %v, want %v", c.name, got, c.want)
		}
	}

	// Predicates AND together.
	got := search(TagFilter{Tag: "p3"}, TagFilter{Tag: "later", Exclude: true})
	if want := []string{"alpha one #p3"}; !slices.Equal(got, want) {
		t.Errorf("AND-ed filters: Search = %v, want %v", got, want)
	}
}

// A tag predicate is applied before LIMIT, so the match can't be cut off by
// better-ranked untagged rows.
func TestSearchTagFilterBeyondLimit(t *testing.T) {
	idx := openTempIndex(t)
	ids := []string{
		"01TAGCCCCCCCCCCCCCCCCCCCC1", "01TAGCCCCCCCCCCCCCCCCCCCC2",
		"01TAGCCCCCCCCCCCCCCCCCCCC3", "01TAGCCCCCCCCCCCCCCCCCCCC4",
	}
	for _, id := range ids {
		if err := idx.Upsert(mkTask(id, "alpha plain", "")); err != nil {
			t.Fatal(err)
		}
	}
	tagged := mkTask("01TAGCCCCCCCCCCCCCCCCCCCC5", "alpha tagged zero #goal", "")
	if err := idx.Upsert(tagged); err != nil {
		t.Fatal(err)
	}

	hits, err := idx.Search("alpha", SearchOpts{Status: "all", Limit: 2, Tags: []TagFilter{{Tag: "goal"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Task.ID != tagged.ID {
		t.Fatalf("Search = %d hits, want exactly the tagged task", len(hits))
	}
}

func TestSearchNegativeLimitIsUnlimited(t *testing.T) {
	idx := openTempIndex(t)
	for i := range 25 {
		id := []byte("01TAGDDDDDDDDDDDDDDDDDDDD0")
		id[len(id)-1] = byte('A' + i)
		if err := idx.Upsert(mkTask(string(id), "alpha bulk", "")); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := idx.Search("alpha", SearchOpts{Status: "all", Limit: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 25 {
		t.Fatalf("Search with negative limit = %d hits, want all 25", len(hits))
	}
}

// Opening a v1 database (no task_tags) migrates it and backfills the mirror
// from the descriptions/details already indexed.
func TestMigrateV2BackfillsTags(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(storage.EnvRoot, dir)
	path := filepath.Join(dir, "index.db")

	idx, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(mkTask("01TAGEEEEEEEEEEEEEEEEEEEE1", "task #One", "and #two words#")); err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(mkTask("01TAGEEEEEEEEEEEEEEEEEEEE2", "no tags here", "")); err != nil {
		t.Fatal(err)
	}
	// Rewind the schema to v1: drop the tag mirror as if written by an older
	// binary.
	for _, stmt := range []string{
		`DROP TRIGGER task_tags_ad`,
		`DROP TABLE task_tags`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := idx.db.Exec(stmt); err != nil {
			t.Fatalf("rewind to v1: %v", err)
		}
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	idx2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen for migration: %v", err)
	}
	t.Cleanup(func() { _ = idx2.Close() })
	want := []string{
		"01TAGEEEEEEEEEEEEEEEEEEEE1:one",
		"01TAGEEEEEEEEEEEEEEEEEEEE1:two words",
	}
	if got := tagRows(t, idx2); !slices.Equal(got, want) {
		t.Fatalf("backfilled tag rows = %v, want %v", got, want)
	}
}
