package tree

import (
	"path/filepath"
	"strings"
	"testing"
)

// renderASCII draws the visible rows so the tables below can state the
// expected shape literally. A repository row is marked with a trailing "*";
// everything unmarked is a directory that exists only to show structure.
func renderASCII(root *Node) string {
	var b strings.Builder
	for _, r := range Flatten(root) {
		b.WriteString(strings.Repeat("  ", r.Depth))
		b.WriteString(r.Node.Name)
		if r.Kind == RowRepo {
			b.WriteString(" *")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// buildTree is Build+Compact with everything expanded, which is how the UI
// assembles a tree before the user has collapsed anything.
func buildTree(root string, mergeLeafRepo bool, repos ...string) *Node {
	abs := make([]string, len(repos))
	for i, r := range repos {
		abs[i] = filepath.Join(root, filepath.FromSlash(r))
	}
	n := Build(root, abs)
	Compact(n, mergeLeafRepo)
	ExpandAll(n)
	return n
}

func TestCompaction(t *testing.T) {
	const root = "/w"
	cases := []struct {
		name  string
		repos []string
		want  string
	}{
		{
			// The whole point: no filler rows, and the repo is not left
			// hanging off an unexplained chain.
			name:  "lone deep repo folds into one row",
			repos: []string{"src/go/projects/myrepo"},
			want:  "src/go/projects/myrepo *\n",
		},
		{
			// Two repos under one chain: the chain still collapses to a single
			// row, but it has to stay a directory because it has two children.
			name:  "shared prefix collapses to one directory row",
			repos: []string{"src/go/a", "src/go/b"},
			want: "src/go\n" +
				"  a *\n" +
				"  b *\n",
		},
		{
			// A real branch point must not be collapsed away.
			name:  "sibling split is preserved",
			repos: []string{"a/x/one", "b/y/two"},
			want: "a/x/one *\n" +
				"b/y/two *\n",
		},
		{
			name:  "directories with no repo below never appear",
			repos: []string{"keep/repo"},
			want:  "keep/repo *\n",
		},
		{
			// A repo that also contains repos is both a row and a parent.
			name:  "nested repo hangs under its parent repo",
			repos: []string{"outer", "outer/inner"},
			want: "outer *\n" +
				"  inner *\n",
		},
		{
			name:  "three way split",
			repos: []string{"p/a", "p/b", "p/c"},
			want: "p\n" +
				"  a *\n" +
				"  b *\n" +
				"  c *\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderASCII(buildTree(root, true, tc.repos...)); got != tc.want {
				t.Errorf("got:\n%swant:\n%s", got, tc.want)
			}
		})
	}
}

func TestCompactionLeafRepoMergeOff(t *testing.T) {
	// With merging off the chain still collapses, but the repository keeps its
	// own row rather than being absorbed into the directory label.
	got := renderASCII(buildTree("/w", false, "src/go/projects/myrepo"))
	want := "src/go/projects\n" +
		"  myrepo *\n"
	if got != want {
		t.Errorf("got:\n%swant:\n%s", got, want)
	}
}

func TestCompactionResplitsWhenSecondRepoArrives(t *testing.T) {
	// Compaction is not stable under streaming discovery. This is the case
	// that forces a full rebuild per batch rather than an incremental update.
	one := renderASCII(buildTree("/w", true, "a/b/repo1"))
	if want := "a/b/repo1 *\n"; one != want {
		t.Fatalf("first:\ngot:\n%swant:\n%s", one, want)
	}
	two := renderASCII(buildTree("/w", true, "a/b/repo1", "a/c/repo2"))
	want := "a\n" +
		"  b/repo1 *\n" +
		"  c/repo2 *\n"
	if two != want {
		t.Errorf("after second repo:\ngot:\n%swant:\n%s", two, want)
	}
}

func TestRootIsARepo(t *testing.T) {
	n := Build("/w", []string{"/w"})
	Compact(n, true)
	// The root is normally not drawn, but a root that is itself a repository
	// must be, or pointing gittree at a repo would show nothing at all.
	if got, want := renderASCII(n), "w *\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRootIsNotDrawnOtherwise(t *testing.T) {
	got := renderASCII(buildTree("/w", true, "a", "b"))
	if want := "a *\nb *\n"; got != want {
		t.Errorf("got:\n%swant:\n%s", got, want)
	}
}

func TestCollapsedNodeHidesChildren(t *testing.T) {
	n := buildTree("/w", true, "p/a", "p/b")
	CollapseAll(n)
	if got, want := renderASCII(n), "p\n"; got != want {
		t.Errorf("collapsed: got %q, want %q", got, want)
	}
}

func TestSortingIsCaseInsensitive(t *testing.T) {
	got := renderASCII(buildTree("/w", true, "p/Zeta", "p/alpha", "p/Beta"))
	want := "p\n  alpha *\n  Beta *\n  Zeta *\n"
	if got != want {
		t.Errorf("got:\n%swant:\n%s", got, want)
	}
}

func TestStateSurvivesRebuild(t *testing.T) {
	first := buildTree("/w", true, "a/b/repo1")
	CollapseAll(first)
	saved := Save(first)

	// The rebuild that a second discovery batch forces.
	second := Build("/w", []string{filepath.FromSlash("/w/a/b/repo1"), filepath.FromSlash("/w/a/c/repo2")})
	Compact(second, true)
	Restore(second, saved)

	idx := Index(second)
	repo1 := idx[filepath.FromSlash("/w/a/b/repo1")]
	if repo1 == nil {
		t.Fatalf("repo1 missing from rebuilt tree; index has %d entries", len(idx))
	}
	if repo1.Expanded {
		t.Error("repo1 should still be collapsed after the rebuild")
	}
	if idx[filepath.FromSlash("/w/a/c/repo2")] == nil {
		t.Error("newly discovered repo2 missing")
	}
}

func TestIndexCoversEveryNode(t *testing.T) {
	n := buildTree("/w", true, "p/a", "p/b")
	idx := Index(n)
	for _, want := range []string{"/w", "/w/p", "/w/p/a", "/w/p/b"} {
		if idx[filepath.FromSlash(want)] == nil {
			t.Errorf("index missing %s", want)
		}
	}
}
