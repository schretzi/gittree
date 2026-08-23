// Package tree arranges discovered repositories into the shape gittree draws:
// enough directory structure to show where things live, and not one row more.
//
// Two rules produce that shape.
//
// Pruning is free by construction: the tree is built only from the paths of
// repositories that were actually found, never from directory listings, so a
// directory with no repository at or below it can never enter it.
//
// Compaction then collapses runs of single-child directories into one row, so
// a repository buried under src/go/projects shows as a single "src/go/projects"
// row above it rather than three rows of scaffolding.
package tree

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/schretzi/gittree/internal/git"
)

// Kind distinguishes a plain directory row from a repository row.
type Kind int

const (
	// KindDir is a directory that exists only to show where repositories sit.
	KindDir Kind = iota
	// KindRepo is a repository.
	KindRepo
)

// Node is one entry in the tree.
type Node struct {
	// Path is the absolute filesystem path, and doubles as the node's stable
	// identity: async scan and fetch results are routed back by it, and
	// expansion state survives a rebuild by it. Never key state on Name, which
	// changes when compaction merges a chain.
	Path string
	// Name is the display label, and may span several path segments once
	// compaction has run, e.g. "src/go/projects".
	Name     string
	Kind     Kind
	Parent   *Node
	Children []*Node
	Expanded bool

	// Repo is the scan result, nil until the repository has been scanned.
	Repo *git.Repo
}

// IsRepo reports whether the node stands for a repository.
func (n *Node) IsRepo() bool { return n != nil && n.Kind == KindRepo }

// Build assembles the tree for repos, all of which must lie at or below root.
//
// Repositories are inserted by path segment, so shared prefixes become shared
// directory nodes; everything else follows from Compact.
func Build(root string, repos []string) *Node {
	root = filepath.Clean(root)
	rootNode := &Node{Path: root, Name: filepath.Base(root), Kind: KindDir, Expanded: true}

	for _, repo := range repos {
		insert(rootNode, root, filepath.Clean(repo))
	}
	sortTree(rootNode)
	return rootNode
}

func insert(root *Node, rootPath, repo string) {
	if repo == rootPath {
		root.Kind = KindRepo
		return
	}
	rel, err := filepath.Rel(rootPath, repo)
	if err != nil || strings.HasPrefix(rel, "..") {
		return // outside the tree; nothing sensible to draw
	}

	cur := root
	curPath := rootPath
	segments := strings.Split(rel, string(filepath.Separator))
	for i, seg := range segments {
		curPath = filepath.Join(curPath, seg)
		child := findChild(cur, seg)
		if child == nil {
			child = &Node{Path: curPath, Name: seg, Kind: KindDir, Parent: cur}
			cur.Children = append(cur.Children, child)
		}
		if i == len(segments)-1 {
			child.Kind = KindRepo
		}
		cur = child
	}
}

func findChild(n *Node, name string) *Node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func sortTree(n *Node) {
	slices.SortFunc(n.Children, func(a, b *Node) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	for _, c := range n.Children {
		sortTree(c)
	}
}

// Compact collapses every chain of single-child directories into one node.
//
// It recurses children-first so a chain of any length folds in a single pass:
// by the time a node is considered, its own subtree has already collapsed as
// far as it can.
//
// mergeLeafRepo also folds a lone repository into the directory chain above
// it, so "src/go/projects" holding only "myrepo" renders as the single row
// "src/go/projects/myrepo" rather than a directory row with one child hanging
// off it. That is the literal reading of "no directories in the tree which are
// not a repo or have one below".
func Compact(n *Node, mergeLeafRepo bool) {
	for _, c := range n.Children {
		Compact(c, mergeLeafRepo)
	}
	// The root keeps its identity: it is the directory the user pointed at,
	// and collapsing it would lose that.
	if n.Kind != KindDir || n.Parent == nil || len(n.Children) != 1 {
		return
	}
	child := n.Children[0]
	if child.IsRepo() && !mergeLeafRepo {
		return
	}
	n.Name = filepath.Join(n.Name, child.Name)
	n.Path = child.Path
	n.Kind = child.Kind
	n.Repo = child.Repo
	n.Children = child.Children
	for _, g := range n.Children {
		g.Parent = n
	}
}

// Index maps every node's path to the node, so an async result can be routed
// to its node in constant time after a rebuild.
func Index(root *Node) map[string]*Node {
	idx := make(map[string]*Node)
	var walk func(*Node)
	walk = func(n *Node) {
		idx[n.Path] = n
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return idx
}

// State is the per-node view state that must survive a rebuild.
//
// Discovery streams in, and compaction is not stable under it: a/b/repo1
// collapses to one row, but the arrival of a/c/repo2 forces a to split again.
// Rebuilding from the full path set each time is microseconds and always
// right; carrying this state across is what makes the rebuild invisible.
type State struct {
	Expanded bool
	Repo     *git.Repo
}

// Save captures the view state of every node, keyed by path.
func Save(root *Node) map[string]State {
	out := make(map[string]State)
	var walk func(*Node)
	walk = func(n *Node) {
		out[n.Path] = State{Expanded: n.Expanded, Repo: n.Repo}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// Restore reapplies saved state to a freshly built tree. Nodes with no saved
// entry keep their defaults, which is what makes newly discovered
// repositories appear collapsed and unscanned.
func Restore(root *Node, saved map[string]State) {
	var walk func(*Node)
	walk = func(n *Node) {
		if s, ok := saved[n.Path]; ok {
			n.Expanded = s.Expanded
			n.Repo = s.Repo
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
}
