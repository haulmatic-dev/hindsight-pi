package core

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// beadsGroupName is the shared group that owns every .beads store the daemon
// must read and write. The convention (documented in
// docs/operations/host-setup.md and enforced root-side by
// packaging/hydra-normalize-beads) is:
//
//	<repo>/.beads            group beads, mode 2770 (setgid, g+rwX, o-rwx)
//	<repo>/.beads/issues.jsonl  group beads, mode 0660 (br init writes 0600)
//	<repo>/.git/worktrees    group beads, g+rwX, o-rwx (hydra-created worktrees)
//
// setgid makes new files inherit the beads group regardless of the creating
// process's primary group — every human's primary group is their own username,
// so without it the inheritance does not happen.
const beadsGroupName = "beads"

// beadsGroupGID resolves the beads group once per process. Not every host has
// the group (dev laptops); a missing group disables onboarding silently.
var beadsGroupGID = func() int {
	g, err := user.LookupGroup(beadsGroupName)
	if err != nil {
		log.Printf("[MultiUserDiscovery] group %q not found — .beads perm onboarding disabled: %v", beadsGroupName, err)
		return -1
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		log.Printf("[MultiUserDiscovery] group %q has non-numeric gid %q — perm onboarding disabled", beadsGroupName, g.Gid)
		return -1
	}
	return gid
}()

// onboardBeadsPerms applies the beads-group permission convention to one repo,
// idempotently. It runs from discovery as the daemon user, so it can only fix
// repos the daemon already owns or is otherwise permitted to chgrp/chmod;
// anything else is logged and skipped (fail-open) — the root-run onboard sweep
// (packaging/hydra-onboard-sweep → hydra-normalize-beads) is the authority for
// other users' files and repairs them on its next cycle.
func onboardBeadsPerms(repoDir string) error {
	if beadsGroupGID < 0 {
		return nil
	}

	beadsDir := filepath.Join(repoDir, ".beads")
	fi, err := os.Stat(beadsDir)
	if err != nil {
		// No .beads store: nothing to onboard (repos without a marker never
		// reach this function, but a race with `br init` is possible).
		return nil
	}
	if !fi.IsDir() {
		return nil
	}

	if beadsPermsConforming(beadsDir, fi) {
		return nil
	}

	if err := applyBeadsGroupWalk(beadsDir); err != nil {
		return err
	}
	// setgid only on the .beads dir itself: new entries inherit the group.
	// Re-stat: the walk just changed the mode, a stale perm would clobber it.
	cur, err := os.Stat(beadsDir)
	if err != nil {
		return fmt.Errorf("stat %s after perm walk: %w", beadsDir, err)
	}
	if err := os.Chmod(beadsDir, cur.Mode()|os.ModeSetgid); err != nil {
		return fmt.Errorf("chmod setgid %s: %w", beadsDir, err)
	}
	log.Printf("[MultiUserDiscovery] Onboarded .beads perms (group %s, g+rwX, setgid): %s", beadsGroupName, beadsDir)

	// hydra-created worktrees land under <repo>/.git/worktrees; make sure the
	// group can share them even if a human created the dir first.
	worktrees := filepath.Join(repoDir, ".git", "worktrees")
	if wfi, err := os.Stat(worktrees); err == nil && wfi.IsDir() && !worktreesPermsConforming(wfi) {
		if err := applyBeadsGroupWalk(worktrees); err != nil {
			return err
		}
		log.Printf("[MultiUserDiscovery] Onboarded .git/worktrees perms: %s", worktrees)
	}
	return nil
}

// worktreesPermsConforming is the lighter convention for <repo>/.git/worktrees:
// beads group, group rwx, world shut out (mode 2770/0770-class; no setgid
// requirement — these dirs are hydra-created and already group-inherit).
func worktreesPermsConforming(fi os.FileInfo) bool {
	perm := fi.Mode().Perm()
	if perm&0o070 != 0o070 || perm&0o007 != 0 {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Gid) == beadsGroupGID
}

// beadsPermsConforming reports whether .beads already follows the convention:
// beads group, setgid, group rwX, world shut out, and issues.jsonl group rw.
// A conforming store is left completely untouched (no chmod, no chown), which
// is what keeps re-discovery idempotent.
func beadsPermsConforming(beadsDir string, dirFi os.FileInfo) bool {
	if !dirPermsConforming(dirFi) {
		return false
	}
	st, ok := dirFi.Sys().(*syscall.Stat_t)
	if !ok || int(st.Gid) != beadsGroupGID {
		return false
	}
	// br init writes issues.jsonl 0600; until it exists there is nothing to fix.
	if fi, err := os.Stat(filepath.Join(beadsDir, "issues.jsonl")); err == nil {
		if fi.Mode().Perm()&0o060 != 0o060 || fi.Mode().Perm()&0o007 != 0 {
			return false
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); !ok || int(st.Gid) != beadsGroupGID {
			return false
		}
	}
	return true
}

// dirPermsConforming is the mode half of the convention: setgid + g+rwX +
// o-rwx on a directory.
func dirPermsConforming(fi os.FileInfo) bool {
	perm := fi.Mode().Perm()
	if fi.Mode()&os.ModeSetgid == 0 {
		return false
	}
	return perm&0o070 == 0o070 && perm&0o007 == 0
}

// applyBeadsGroupWalk chgrps every entry under root to the beads group and
// applies g+rwX,o-rwx (the X form: execute only where already present or on
// directories). Individual failures are collected and reported but do not
// abort the walk — a single unreadable file must not block the rest.
func applyBeadsGroupWalk(root string) error {
	var firstErr error
	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			log.Printf("[MultiUserDiscovery] perm walk %s (fail-open): %v", path, err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if err := os.Chown(path, -1, beadsGroupGID); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("chgrp %s: %w", path, err)
			log.Printf("[MultiUserDiscovery] %v (fail-open)", firstErr)
		}
		perm := fi.Mode().Perm()
		newPerm := perm&0o770 | 0o060
		newPerm &^= 0o007
		if d.IsDir() || perm&0o111 != 0 {
			newPerm |= 0o010
		}
		if newPerm != perm {
			if err := os.Chmod(path, newPerm); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("chmod %s: %w", path, err)
				log.Printf("[MultiUserDiscovery] %v (fail-open)", firstErr)
			}
		}
		return nil
	}
	if err := filepath.WalkDir(root, walkFn); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}
