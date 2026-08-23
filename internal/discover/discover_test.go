package discover

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// build materialises a directory layout under a temp dir. A path ending in
// "/.git" becomes a directory; a path of the form "p/.git=>target" becomes a
// .git *file* pointing at target, which is how linked worktrees and submodules
// announce themselves.
func build(t *testing.T, layout ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, spec := range layout {
		path, target, isFile := strings.Cut(spec, "=>")
		full := filepath.Join(root, filepath.FromSlash(path))
		if isFile {
			if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte("gitdir: "+target+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(full, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// found runs a walk and returns the discovered directories relative to root,
// sorted, so assertions are order-independent.
func found(t *testing.T, root string, tweak func(*Options)) []string {
	t.Helper()
	o := Defaults(root)
	if tweak != nil {
		tweak(&o)
	}
	var got []string
	if _, err := Walk(context.Background(), o, func(f Found) {
		rel, err := filepath.Rel(root, f.Dir)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, filepath.ToSlash(rel))
	}); err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	return got
}

func TestWalkFindsRepos(t *testing.T) {
	root := build(t,
		"a/repo1/.git",
		"a/repo1/src",
		"b/c/repo2/.git",
		"b/empty/nothing/here",
		"loose-file-dir",
	)
	want := []string{"a/repo1", "b/c/repo2"}
	if got := found(t, root, nil); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWalkDetectsGitFile(t *testing.T) {
	// No repository on this machine uses a .git file, so this case exists only
	// synthetically -- which is exactly why it needs a test.
	root := build(t,
		"linked/.git=>/somewhere/.git/worktrees/wt1",
		"sub/.git=>/somewhere/.git/modules/sub",
		"plain/.git",
	)
	got := found(t, root, nil)
	want := []string{"linked", "plain", "sub"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	kinds := map[string]Kind{}
	if _, err := Walk(context.Background(), Defaults(root), func(f Found) {
		kinds[filepath.Base(f.Dir)] = f.Kind
	}); err != nil {
		t.Fatal(err)
	}
	if kinds["linked"] != KindLinked {
		t.Errorf("linked kind = %v, want KindLinked", kinds["linked"])
	}
	if kinds["sub"] != KindSubmodule {
		t.Errorf("sub kind = %v, want KindSubmodule", kinds["sub"])
	}
	if kinds["plain"] != KindWorktree {
		t.Errorf("plain kind = %v, want KindWorktree", kinds["plain"])
	}
}

func TestWalkDoesNotDescendIntoReposByDefault(t *testing.T) {
	root := build(t, "outer/.git", "outer/vendor/inner/.git", "outer/sub/.git")
	if got, want := found(t, root, nil), []string{"outer"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// --nested opts in, but the ignore list still applies, so the vendored
	// repository stays hidden while the real submodule shows up.
	got := found(t, root, func(o *Options) { o.Nested = true })
	if want := []string{"outer", "outer/sub"}; !slices.Equal(got, want) {
		t.Errorf("nested: got %v, want %v", got, want)
	}
}

func TestWalkSkipsIgnoredDirs(t *testing.T) {
	root := build(t, "node_modules/pkg/.git", "keep/.git", ".config/dots/.git")
	// Dot-directories are not skipped: ~/.config holds real repositories.
	got := found(t, root, nil)
	if want := []string{".config/dots", "keep"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWalkRespectsMaxDepth(t *testing.T) {
	root := build(t, "a/b/c/d/deep/.git", "a/shallow/.git")
	got := found(t, root, func(o *Options) { o.MaxDepth = 2 })
	if want := []string{"a/shallow"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWalkRootItselfIsARepo(t *testing.T) {
	root := build(t, ".git", "sub/other/.git")
	if got, want := found(t, root, nil), []string{"."}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWalkSkipsBareByDefault(t *testing.T) {
	root := build(t, "bare.git/objects", "bare.git/refs", "bare.git/HEAD/x", "real/.git")
	if got, want := found(t, root, nil), []string{"real"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWalkCancellation(t *testing.T) {
	root := build(t, "a/.git", "b/.git", "c/.git")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Walk(ctx, Defaults(root), func(Found) {}); err == nil {
		t.Fatal("want a context error from a cancelled walk")
	}
}
