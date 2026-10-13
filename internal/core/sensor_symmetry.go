package core

import (
	"fmt"
)

// Sensor symmetry (shepherd observability): every detector emits a
// fire-resolve pair of typed events under a shared correlation ID, so the
// daily rollup (scripts/sensor-rollup.sh) can compute per-sensor
// fire/resolve rates — the fine-tuning evidence that was previously
// unmeasurable (OrphanRetry 1,912 fires / 0 resolves; AIMD invisible).
// All emissions are fail-open: the event stream observes the system and
// must never gate it.

// wrapRepairHandlers returns a copy of the Pan repair handlers that emits
// repair_failed (fire) / repair_succeeded (resolve) for each verb under the
// pan-repair correlation ID. The original handler's error return is
// preserved verbatim; emissions are nil-sink-safe and swallow marshal/log
// failure silently per the event-stream contract.
func wrapRepairHandlers(handlers map[string]func(beadID string) error, sink *EventSink) map[string]func(beadID string) error {
	if handlers == nil {
		return nil
	}
	wrapped := make(map[string]func(beadID string) error, len(handlers))
	for verb, fn := range handlers {
		verbName := verb
		wrapped[verbName] = func(beadID string) error {
			err := fn(beadID)
			if sink != nil {
				evType, reason := "repair_failed", "error"
				detail := fmt.Sprintf("pan repair %s for %s failed: %v", verbName, beadID, err)
				if err == nil {
					evType, reason = "repair_succeeded", "ok"
					detail = fmt.Sprintf("pan repair %s succeeded for %s", verbName, beadID)
				}
				sink.Emit(Event{Type: evType, Reason: reason, Bead: beadID,
					Detail: detail,
					Fields: map[string]any{
						"verb":           verbName,
						"correlation_id": SensorCorrelation("pan-repair", verbName+":"+beadID),
					}})
			}
			return err
		}
	}
	return wrapped
}
