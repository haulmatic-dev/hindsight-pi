package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/haulmatic-dev/hydra/internal/beads"
	"github.com/haulmatic-dev/hydra/internal/core"
	"github.com/haulmatic-dev/hydra/internal/registry"
	"github.com/haulmatic-dev/hydra/pkg/types"
)

const (
	statusWorkerLimit  = 8
	statusVerdictLimit = 6
	statusReadyLimit   = 3
)

func runStatusSubcommand(configPath string) {
	config, err := core.LoadConfig(configPath)
	if err != nil {
		config = core.DefaultConfig()
	}
	writeStatusReport(config, os.Stdout)
}

func writeStatusReport(config *core.Config, w io.Writer) {
	now := time.Now()

	// Workers
	fmt.Fprintln(w, "Workers")
	fmt.Fprintln(w, "-------")
	workers, workerErr := loadWorkers()
	if workerErr != nil {
		fmt.Fprintf(w, "  (registry unavailable: %v)\n", workerErr)
	} else if len(workers) == 0 {
		fmt.Fprintln(w, "  no workers")
	} else {
		sort.Slice(workers, func(i, j int) bool {
			return workers[i].LastHeartbeat.After(workers[j].LastHeartbeat)
		})
		if len(workers) > statusWorkerLimit {
			workers = workers[:statusWorkerLimit]
		}
		for _, wr := range workers {
			age := now.Sub(wr.StartedAt).Truncate(time.Second)
			exit := "-"
			if wr.ExitCode != 0 {
				exit = strconv.Itoa(wr.ExitCode)
			}
			tokens := humanTokens(wr.TokensSoFar)
			fmt.Fprintf(w, "  %-30s %-10s %8s exit=%-4s age=%s\n", wr.BeadID, wr.State, tokens, exit, age)
		}
	}
	fmt.Fprintln(w)

	// Recent verdicts
	fmt.Fprintln(w, "Recent verdicts")
	fmt.Fprintln(w, "---------------")
	logPath := config.Status.LogPath
	if logPath == "" {
		logPath = "/var/log/hydra/hydra-error.log"
	}
	verdicts, verdictErr := recentVerdicts(logPath, statusVerdictLimit)
	if verdictErr != nil {
		fmt.Fprintf(w, "  (log unavailable: %v)\n", verdictErr)
	} else if len(verdicts) == 0 {
		fmt.Fprintln(w, "  no recent verdicts")
	} else {
		for _, v := range verdicts {
			fmt.Fprintf(w, "  %s\n", v)
		}
	}
	fmt.Fprintln(w)

	// Boards
	fmt.Fprintln(w, "Boards")
	fmt.Fprintln(w, "------")
	if len(config.Projects) == 0 {
		fmt.Fprintln(w, "  no projects configured")
	} else {
		for _, p := range config.Projects {
			repos := p.Repos
			if len(repos) == 0 {
				repos = []core.RepoConfig{{Name: "", Path: p.Path}}
			}
			for _, r := range repos {
				cwd := repoCwd(p.Path, r.Name)
				open, inProgress, ready, boardErr := boardSnapshot(cwd)
				if boardErr != nil {
					fmt.Fprintf(w, "  %s: board unavailable: %v\n", p.Name, boardErr)
					continue
				}
				fmt.Fprintf(w, "  %s: open=%d in_progress=%d ready=%d\n", p.Name, open, inProgress, len(ready))
				for i, b := range ready {
					if i >= statusReadyLimit {
						break
					}
					fmt.Fprintf(w, "    [%s] %s (P%d)\n", b.ID, truncate(b.Title, 50), b.Priority)
				}
			}
		}
	}
	fmt.Fprintln(w)

	// Queue health
	fmt.Fprintln(w, "Queue health")
	fmt.Fprintln(w, "------------")
	health := queueHealth(config, workers)
	fmt.Fprintf(w, "  pool utilisation: %s\n", health.utilisation)
	fmt.Fprintf(w, "  parked-stale-heartbeat: %d\n", health.parkedStale)
	fmt.Fprintf(w, "  starved-open-P1: %d\n", health.starvedP1)
}

type queueHealthReport struct {
	utilisation string
	parkedStale int
	starvedP1   int
}

func queueHealth(config *core.Config, workers []registry.WorkerRecord) queueHealthReport {
	r := queueHealthReport{utilisation: "n/a"}

	maxWorkers := config.Overseer.MaxWorkers
	if maxWorkers > 0 {
		active := 0
		for _, w := range workers {
			if w.State == registry.StateWorking || w.State == registry.StateParked {
				active++
			}
		}
		r.utilisation = fmt.Sprintf("%d/%d (%d%%)", active, maxWorkers, active*100/maxWorkers)
	}

	threshold := time.Duration(config.Shepherd.Pan.HungAfterSeconds) * time.Second
	if threshold <= 0 {
		threshold = 5 * time.Minute
	}
	cutoff := time.Now().Add(-threshold)

	workerSet := make(map[string]struct{}, len(workers))
	for _, w := range workers {
		workerSet[w.BeadID] = struct{}{}
		if w.State == registry.StateParked && w.LastHeartbeat.Before(cutoff) {
			r.parkedStale++
		}
	}

	r.starvedP1 = countStarvedOpenP1(config, workerSet)
	return r
}

func countStarvedOpenP1(config *core.Config, workerSet map[string]struct{}) int {
	parser := beads.NewParser()
	count := 0
	for _, p := range config.Projects {
		repos := p.Repos
		if len(repos) == 0 {
			repos = []core.RepoConfig{{Name: "", Path: p.Path}}
		}
		for _, r := range repos {
			cwd := repoCwd(p.Path, r.Name)
			out, err := runInDir(cwd, "br", "list", "--status", "open", "--json")
			if err != nil {
				continue
			}
			beadList, err := parser.ParseBeadList(out, beads.BeadContext{Project: p.Name, Repo: r.Name})
			if err != nil {
				continue
			}
			for _, b := range beadList {
				if b.Status == types.StatusOpen && b.Priority == 1 {
					if _, ok := workerSet[string(b.ID)]; !ok {
						count++
					}
				}
			}
		}
	}
	return count
}

func boardSnapshot(cwd string) (open, inProgress int, ready []types.Bead, err error) {
	parser := beads.NewParser()
	ctx := beads.BeadContext{}
	open, err = countStatus(cwd, "open", parser, ctx)
	if err != nil {
		return 0, 0, nil, err
	}
	inProgress, err = countStatus(cwd, "in_progress", parser, ctx)
	if err != nil {
		return 0, 0, nil, err
	}
	out, err := runInDir(cwd, "br", "ready", "--json", "--limit", strconv.Itoa(statusReadyLimit), "--sort", "priority")
	if err != nil {
		return 0, 0, nil, err
	}
	ready, err = parser.ParseReadyBeads(out, ctx)
	if err != nil {
		return 0, 0, nil, err
	}
	return open, inProgress, ready, nil
}

func countStatus(cwd, status string, parser *beads.Parser, ctx beads.BeadContext) (int, error) {
	out, err := runInDir(cwd, "br", "list", "--status", status, "--json")
	if err != nil {
		return 0, err
	}
	beadList, err := parser.ParseBeadList(out, ctx)
	if err != nil {
		return 0, err
	}
	return len(beadList), nil
}

func runInDir(cwd, name string, arg ...string) ([]byte, error) {
	cmd := exec.Command(name, arg...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	return cmd.Output()
}

func repoCwd(projectPath, repoName string) string {
	if repoName != "" {
		return filepath.Join(projectPath, repoName)
	}
	return projectPath
}

func loadWorkers() ([]registry.WorkerRecord, error) {
	store, err := registry.Open(core.RegistryPath())
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListWorkers()
}

func recentVerdicts(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	markers := []string{"[CloseEvent]", "[PanRepair]"}
	var matched []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		for _, m := range markers {
			if strings.Contains(line, m) {
				matched = append(matched, strings.TrimSpace(line))
				break
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(matched) > n {
		matched = matched[len(matched)-n:]
	}
	return matched, nil
}

func humanTokens(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
