package tree

import "github.com/schretzi/gittree/internal/git"

// RowKind is what a visible line stands for.
type RowKind int

const (
	// RowDir is a directory row.
	RowDir RowKind = iota
	// RowRepo is a repository row.
	RowRepo
	// RowBranch is one local branch of the repository above it.
	RowBranch
)

// Row is one visible line of the tree.
//
// Branch rows are synthesised here rather than stored as nodes. That is the
// decoupling that keeps async updates cheap: a scan result replaces
// node.Repo and the tree is re-flattened, with no node created, destroyed or
// re-parented, so nothing holding a node pointer is invalidated.
type Row struct {
	Node   *Node
	Kind   RowKind
	Depth  int
	Branch *git.Branch // set only when Kind is RowBranch
	// ID identifies the row across rebuilds, so the cursor can stay on the
	// same thing when rows appear above it.
	ID string
}

// Flatten produces the visible rows, honouring each node's expansion state.
//
// The root is not drawn: it is the directory the user pointed gittree at, and
// a row for "." carries nothing. Its children start at depth zero. The one
// exception is a root that is itself a repository, which obviously must show.
func Flatten(root *Node) []Row {
	var rows []Row
	if root == nil {
		return rows
	}
	if root.IsRepo() {
		appendNode(&rows, root, 0)
		return rows
	}
	for _, c := range root.Children {
		appendNode(&rows, c, 0)
	}
	return rows
}

func appendNode(rows *[]Row, n *Node, depth int) {
	kind := RowDir
	if n.IsRepo() {
		kind = RowRepo
	}
	*rows = append(*rows, Row{Node: n, Kind: kind, Depth: depth, ID: n.Path})

	if !n.Expanded {
		return
	}
	if n.IsRepo() && n.Repo != nil {
		for i := range n.Repo.Branches {
			br := &n.Repo.Branches[i]
			*rows = append(*rows, Row{
				Node:   n,
				Kind:   RowBranch,
				Depth:  depth + 1,
				Branch: br,
				// A NUL keeps the branch id unambiguous: it is the one byte
				// that can appear in neither a path nor a branch name.
				ID: n.Path + "\x00" + br.Name,
			})
		}
	}
	for _, c := range n.Children {
		appendNode(rows, c, depth+1)
	}
}

// HasChildren reports whether a node can be expanded at all -- either it has
// child nodes, or it is a scanned repository with branches to show.
func (n *Node) HasChildren() bool {
	if len(n.Children) > 0 {
		return true
	}
	return n.IsRepo() && n.Repo != nil && len(n.Repo.Branches) > 0
}

// AutoExpand opens repositories that have something worth seeing when opened.
//
// A repository with a single branch says everything it has to say on its own
// collapsed row, so expanding it by default would add a line and no
// information. Repositories with several branches, or with a branch checked
// out in more than one worktree, do have more to show.
func AutoExpand(root *Node) {
	var walk func(*Node)
	walk = func(n *Node) {
		if n.IsRepo() && n.Repo != nil && len(n.Repo.Branches) > 1 {
			n.Expanded = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
}

// ExpandAll and CollapseAll set every expandable node's state at once.
func ExpandAll(root *Node)   { setExpanded(root, true) }
func CollapseAll(root *Node) { setExpanded(root, false) }

func setExpanded(n *Node, v bool) {
	for _, c := range n.Children {
		setExpanded(c, v)
	}
	if n.Parent != nil {
		n.Expanded = v
	}
	if n.IsRepo() {
		n.Expanded = v
	}
}
