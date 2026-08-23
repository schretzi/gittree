package git

import (
	"bytes"
	"strconv"
	"strings"
	"time"
)

// fieldSep is the separator used in the for-each-ref format string. git
// expands %00 to a real NUL, which is the only byte that cannot occur in a
// branch name -- unlike '|', which can. Records stay newline-separated.
const fieldSep = 0

// forEachRefFormat lists, per local branch, everything gittree needs. Getting
// it in one invocation is what keeps a scan to three processes.
//
// %(worktreepath) is what makes the branch-to-worktree mapping free; it needs
// git 2.29 or newer, and is empty for a branch that is not checked out.
const forEachRefFormat = "%(HEAD)%00" +
	"%(refname:short)%00" +
	"%(upstream:short)%00" +
	"%(upstream:track)%00" +
	"%(upstream:trackshort)%00" +
	"%(committerdate:unix)%00" +
	"%(objectname:short)%00" +
	"%(worktreepath)"

// parseForEachRef turns the output of for-each-ref into branches.
//
// Lines with too few fields are skipped rather than failing the whole scan: a
// single malformed ref should not blank out an otherwise readable repository.
func parseForEachRef(b []byte) []Branch {
	var out []Branch
	for line := range bytes.Lines(b) {
		line = bytes.TrimRight(line, "\r\n")
		if len(line) == 0 {
			continue
		}
		f := bytes.Split(line, []byte{fieldSep})
		if len(f) < 8 {
			continue
		}
		br := Branch{
			Name:     string(f[1]),
			Upstream: string(f[2]),
			Head:     string(f[0]) == "*",
			Commit:   string(f[6]),
			Worktree: string(f[7]),
		}
		br.Sync, br.Ahead, br.Behind = deriveSync(string(f[2]), string(f[3]))
		if secs, err := strconv.ParseInt(string(f[5]), 10, 64); err == nil {
			br.When = time.Unix(secs, 0)
		}
		out = append(out, br)
	}
	return out
}

// deriveSync maps an upstream name and %(upstream:track) onto a SyncState.
//
// The counts are taken from track rather than from %(upstream:trackshort)
// because they are the same source of truth the state is derived from, so the
// two can never disagree.
func deriveSync(upstream, track string) (SyncState, int, int) {
	if upstream == "" {
		return SyncNoUpstream, 0, 0
	}
	if track == "[gone]" {
		return SyncGone, 0, 0
	}
	ahead, behind := parseAheadBehind(track)
	switch {
	case ahead > 0 && behind > 0:
		return SyncDiverged, ahead, behind
	case ahead > 0:
		return SyncAhead, ahead, 0
	case behind > 0:
		return SyncBehind, 0, behind
	default:
		return SyncInSync, 0, 0
	}
}

// parseAheadBehind reads the counts out of "[ahead 1, behind 2]", "[ahead 3]",
// "[behind 5]" or "". Anything it does not recognise counts as zero.
func parseAheadBehind(track string) (ahead, behind int) {
	track = strings.TrimSuffix(strings.TrimPrefix(track, "["), "]")
	for part := range strings.SplitSeq(track, ",") {
		fields := strings.Fields(part)
		if len(fields) != 2 {
			continue
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		switch fields[0] {
		case "ahead":
			ahead = n
		case "behind":
			behind = n
		}
	}
	return ahead, behind
}

// parseStatusV2 counts the working tree's changes from
// "status --porcelain=v2 --branch".
//
// Splitting on newlines is safe here precisely because -z is not used: git
// C-quotes any path containing a newline, so no record can span a line. The
// counts never involve a path anyway.
//
// An absent "# branch.upstream" header is the no-upstream case, which is
// ordinary and not an error -- four of the ten repositories this was designed
// against are in it.
func parseStatusV2(b []byte) Status {
	s := Status{}
	for line := range bytes.Lines(b) {
		f := strings.Fields(string(bytes.TrimRight(line, "\r\n")))
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "#":
			parseStatusHeader(&s, f)
		case "1", "2":
			// Field 1 is the two-character XY code: X is the change staged in
			// the index, Y the change in the working tree. '.' means unchanged,
			// and a path can count towards both.
			if len(f) < 2 || len(f[1]) < 2 {
				continue
			}
			s.Files++
			if f[1][0] != '.' {
				s.Staged++
			}
			if f[1][1] != '.' {
				s.Modified++
			}
		case "u":
			s.Conflicted++
			s.Files++
		case "?":
			s.Untracked++
			s.Files++
		}
	}
	return s
}

// parseStatusHeader reads one "# branch.*" line of the status output.
func parseStatusHeader(s *Status, f []string) {
	if len(f) < 3 {
		return
	}
	switch f[1] {
	case "branch.head":
		s.Head = f[2]
	case "branch.oid":
		// The literal string "(initial)" is git's way of saying the branch has
		// no commits yet, so there is no ref for it to appear under.
		s.Unborn = f[2] == "(initial)"
	case "branch.upstream":
		s.Upstream = f[2]
	case "branch.ab":
		if len(f) >= 4 {
			s.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[2], "+"))
			s.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[3], "-"))
		}
	}
}

// worktree is one entry of "git worktree list --porcelain".
type worktree struct {
	Dir      string
	Branch   string // short name; empty when detached
	Detached bool
}

// parseWorktreeList reads "git worktree list --porcelain". gittree only needs
// it to find *detached* worktrees, since %(worktreepath) already reports every
// worktree that has a branch checked out.
func parseWorktreeList(b []byte) []worktree {
	var (
		out []worktree
		cur *worktree
	)
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for line := range bytes.Lines(b) {
		text := string(bytes.TrimRight(line, "\r\n"))
		switch {
		case strings.HasPrefix(text, "worktree "):
			flush()
			cur = &worktree{Dir: strings.TrimPrefix(text, "worktree ")}
		case cur == nil:
			continue
		case strings.HasPrefix(text, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(text, "branch "), "refs/heads/")
		case text == "detached":
			cur.Detached = true
		}
	}
	flush()
	return out
}
