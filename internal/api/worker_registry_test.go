package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/haulmatic-dev/hydra/internal/registry"
)

func setupRegistryRouter(t *testing.T, store *registry.Store) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	RegisterWorkerRegistryRoutes(v1, func() *registry.Store { return store })
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func openTestStore(t *testing.T) *registry.Store {
	t.Helper()
	st, err := registry.Open(":memory:")
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func TestWorkerReportRoundTrip(t *testing.T) {
	store := openTestStore(t)
	srv := setupRegistryRouter(t, store)

	resp := postJSON(t, srv.URL+"/api/v1/workers/report", registry.WorkerReport{
		BeadID: "bead-1", PID: 42, PGID: 42, Project: "proj",
		State: "working", PiSessionID: "sess-1", TokensSoFar: 1000,
	})
	if resp.StatusCode != 200 {
		t.Fatalf("report: %d", resp.StatusCode)
	}
	resp.Body.Close()

	got, ok, err := store.GetWorker("bead-1")
	if err != nil || !ok {
		t.Fatalf("GetWorker: ok=%v err=%v", ok, err)
	}
	if got.State != registry.StateWorking || got.PID != 42 || got.TokensSoFar != 1000 {
		t.Errorf("record mismatch: %+v", got)
	}

	// Second report: StartedAt preserved, heartbeat advanced.
	first := got.StartedAt
	resp = postJSON(t, srv.URL+"/api/v1/workers/report", registry.WorkerReport{
		BeadID: "bead-1", PID: 42, State: "parked", TokensSoFar: 2000,
		ParkReason: "provider-capacity",
	})
	resp.Body.Close()
	got, _, _ = store.GetWorker("bead-1")
	if got.State != registry.StateParked || got.ParkReason != "provider-capacity" {
		t.Errorf("park report not applied: %+v", got)
	}
	if !got.StartedAt.Equal(first) {
		t.Errorf("StartedAt changed across reports: %v → %v", first, got.StartedAt)
	}
}

func TestWorkerReportValidation(t *testing.T) {
	srv := setupRegistryRouter(t, openTestStore(t))

	resp := postJSON(t, srv.URL+"/api/v1/workers/report", registry.WorkerReport{State: "working"})
	if resp.StatusCode != 400 {
		t.Errorf("missing bead_id: %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	resp = postJSON(t, srv.URL+"/api/v1/workers/report", registry.WorkerReport{BeadID: "b", State: "completed"})
	if resp.StatusCode != 400 {
		t.Errorf("workers must not self-report completed via report endpoint: %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestWorkerCompletedEndpoint(t *testing.T) {
	store := openTestStore(t)
	srv := setupRegistryRouter(t, store)

	postJSON(t, srv.URL+"/api/v1/workers/report", registry.WorkerReport{BeadID: "b1", State: "working"}).Body.Close()
	resp := postJSON(t, srv.URL+"/api/v1/workers/completed", registry.CompletionReport{BeadID: "b1", ExitCode: 0})
	if resp.StatusCode != 200 {
		t.Fatalf("completed: %d", resp.StatusCode)
	}
	resp.Body.Close()

	got, _, _ := store.GetWorker("b1")
	if got.State != registry.StateCompleted || got.ExitCode != 0 {
		t.Errorf("not completed: %+v", got)
	}
}

func TestCommandsConsumeOnRead(t *testing.T) {
	store := openTestStore(t)
	srv := setupRegistryRouter(t, store)

	store.EnqueueCommand("b1", registry.CmdSteer, "verdict missing")
	store.EnqueueCommand("b1", registry.CmdContinue, "")

	resp, err := http.Get(srv.URL + "/api/v1/workers/commands?bead_id=b1")
	if err != nil {
		t.Fatalf("GET commands: %v", err)
	}
	var body struct {
		Commands []registry.Command `json:"commands"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if len(body.Commands) != 2 || body.Commands[0].Kind != registry.CmdSteer {
		t.Fatalf("commands: %+v", body.Commands)
	}

	// Second poll: empty (consumed).
	resp, _ = http.Get(srv.URL + "/api/v1/workers/commands?bead_id=b1")
	body.Commands = nil
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if len(body.Commands) != 0 {
		t.Errorf("commands not consumed: %+v", body.Commands)
	}
}

func TestRegistryUnavailable(t *testing.T) {
	srv := setupRegistryRouter(t, nil)
	resp := postJSON(t, srv.URL+"/api/v1/workers/report", registry.WorkerReport{BeadID: "b", State: "working"})
	if resp.StatusCode != 503 {
		t.Errorf("nil store: %d, want 503", resp.StatusCode)
	}
	resp.Body.Close()
}
