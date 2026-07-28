package main

import (
	"os"
	"path/filepath"
)

// resolvePiAgentDir returns the directory to advertise as PI_CODING_AGENT_DIR:
// the one that actually holds Pi's package set.
//
// Two layouts exist in practice and they differ by exactly one level:
//
//	~/.pi/agent/{settings.json, npm/node_modules}                 (developer machine)
//	/opt/dev-tools/config/pi-worker/{settings.json, npm/node_modules}  (VPS worker config)
//
// The worker used to append "agent" unconditionally. On the VPS layout that names
// a directory containing neither, and Pi then reports "No packages installed" —
// so every worker ran with **zero extensions**: no rtk command rewriting, no
// context-mode, no caveman, no guardrails, regardless of protocol mode.
//
// Verified on the box, same binary, only this variable changed:
//
//	PI_CODING_AGENT_DIR=<cfg>/agent   -> pi list: "No packages installed."
//	PI_CODING_AGENT_DIR=<cfg>         -> pi list: 13 packages, incl. pi-rtk-optimizer@0.9.0
//
// This is why rtk's own savings counter had not moved since 06-26: the workers
// that do nearly all of Hydra's tool calls never had rtk loaded at all.
//
// <cfg>/agent is probed first so the developer-machine layout keeps working.
func resolvePiAgentDir(configDir string) string {
	if configDir == "" {
		return ""
	}

	for _, candidate := range []string{filepath.Join(configDir, "agent"), configDir} {
		if hasPiPackageRoot(candidate) {
			return absOrSelf(candidate)
		}
	}

	// Neither candidate looks like a package root. Keep the historical value so
	// this cannot change behaviour in setups it does not understand.
	return absOrSelf(filepath.Join(configDir, "agent"))
}

// hasPiPackageRoot reports whether dir looks like a Pi package root — it holds
// the settings file listing packages, or the installed packages themselves.
func hasPiPackageRoot(dir string) bool {
	if fi, err := os.Stat(filepath.Join(dir, "settings.json")); err == nil && !fi.IsDir() {
		return true
	}
	if fi, err := os.Stat(filepath.Join(dir, "npm", "node_modules")); err == nil && fi.IsDir() {
		return true
	}
	return false
}

func absOrSelf(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
