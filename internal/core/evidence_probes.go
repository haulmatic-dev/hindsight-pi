package core

import (
	"time"
)

// recentWorkerActivityProbe returns the activity probe for the evidence
// sweep: true when the registry holds a worker row for the project whose
// heartbeat is fresh within the evidence window (24h). Nil registry fails
// open to "no activity" — the sweep then stays quiet rather than filing
// false degradations.
func (o *Overseer) recentWorkerActivityProbe() func(project string) bool {
	return func(project string) bool {
		if o.registryStore == nil {
			return false
		}
		rows, err := o.registryStore.ListWorkers()
		if err != nil {
			return false
		}
		for _, rec := range rows {
			if rec.Project == project && rec.LastHeartbeat.After(time.Now().Add(-evidenceActivityWindow)) {
				return true
			}
		}
		return false
	}
}

// hindsightMemoriesProbe returns the memory-count probe for the evidence
// sweep: the number of memories the Hindsight client serves for a project
// query. Nil/disabled client reports 0 with nil error — the sweep then treats
// an active project as degraded, which is exactly the silent-degradation
// condition this tier exists to surface. A live query error is reported as an
// error so scan() can fail open per-probe.
func (o *Overseer) hindsightMemoriesProbe() func(project string) (int, error) {
	return func(project string) (int, error) {
		client := o.hindsightClient
		if client == nil || !client.IsEnabled() {
			return 0, nil
		}
		resp, err := client.Search(project, "", 5)
		if err != nil {
			return 0, err
		}
		count := len(resp.Memories) + len(resp.Results)
		if count == 0 {
			count = resp.Count
		}
		return count, nil
	}
}
