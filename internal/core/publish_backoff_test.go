package core

import (
	"testing"
	"time"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func newBackoffTestOverseer() *Overseer {
	o := &Overseer{}
	o.pushPending = make(map[string]*PushPendingEntry)
	return o
}

func TestPublishBackoff_ThrottlesFailingProject(t *testing.T) {
	o := newBackoffTestOverseer()
	p := types.Project{Name: "srv-telematics", Path: "/home/ajantha/projects/srv-telematics"}

	o.notePublishBackoff(p.Path, "pr merge failed: TRANSIENT")
	if !o.publishBackoffActive(p.Path) {
		t.Fatal("project must be in backoff immediately after a failure")
	}
	// other projects unaffected
	if o.publishBackoffActive("/other/project") {
		t.Fatal("backoff must be per-project")
	}

	// exponential growth: repeated failures extend the cooldown
	o.pushPendingMu.Lock()
	first := o.publishBackoff[p.Path].next
	o.pushPendingMu.Unlock()
	o.notePublishBackoff(p.Path, "pr merge failed: TRANSIENT")
	o.pushPendingMu.Lock()
	second := o.publishBackoff[p.Path].next
	o.pushPendingMu.Unlock()
	if !second.After(first) {
		t.Fatalf("backoff must grow: first=%v second=%v", first, second)
	}
}

func TestPublishBackoff_ClearsOnSuccess(t *testing.T) {
	o := newBackoffTestOverseer()
	path := "/opt/dev-tools/components/hydra"
	o.notePublishBackoff(path, "pr merge failed")
	o.clearPublishBackoff(path)
	if o.publishBackoffActive(path) {
		t.Fatal("clearPublishBackoff must reset the cooldown")
	}
	// clearing an unknown project is a no-op
	o.clearPublishBackoff("/never/backed/off")
}

func TestPublishBackoff_CapAndNilSafety(t *testing.T) {
	o := newBackoffTestOverseer()
	path := "/cap/test"
	for i := 0; i < 30; i++ {
		o.notePublishBackoff(path, "pr merge failed")
	}
	o.pushPendingMu.Lock()
	attempt := o.publishBackoff[path].attempt
	o.pushPendingMu.Unlock()
	if attempt != 30 {
		t.Fatalf("attempt count = %d, want 30", attempt)
	}
	// entry must still exist and be active; cap keeps interval bounded
	o.pushPendingMu.Lock()
	d := time.Until(o.publishBackoff[path].next)
	o.pushPendingMu.Unlock()
	if d > publishBackoffMax+time.Second {
		t.Fatalf("backoff exceeded cap: %v", d)
	}
	// nil receiver safety
	var nilO *Overseer
	nilO.notePublishBackoff(path, "x")
	nilO.clearPublishBackoff(path)
	if nilO.publishBackoffActive(path) {
		t.Fatal("nil overseer must never report backoff")
	}
}
