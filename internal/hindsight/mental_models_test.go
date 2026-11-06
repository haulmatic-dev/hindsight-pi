package hindsight

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetMentalModelsFiltersEmptyContent(t *testing.T) {
	// The LIST endpoint projects content as empty; real content lives in the
	// per-model DETAIL endpoint. The client must stitch list -> detail.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/default/banks/coding-brain/mental-models" {
			_, _ = w.Write([]byte(`{"items":[
				{"id":"mm-1","name":"Has content","content":""},
				{"id":"mm-2","name":"Never refreshed","content":""}
			],"total":2,"limit":100,"offset":0}`))
			return
		}
		switch r.URL.Path {
		case "/v1/default/banks/coding-brain/mental-models/mm-1":
			_, _ = w.Write([]byte(`{"id":"mm-1","name":"Has content","content":"rule A\nrule B"}`))
		case "/v1/default/banks/coding-brain/mental-models/mm-2":
			_, _ = w.Write([]byte(`{"id":"mm-2","name":"Never refreshed","content":""}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithEnabled(true))
	models, err := c.GetMentalModels(context.Background())
	if err != nil {
		t.Fatalf("GetMentalModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("want 1 non-empty model, got %d: %+v", len(models), models)
	}
	if models[0].Name != "Has content" || models[0].Content != "rule A\nrule B" {
		t.Fatalf("unexpected model: %+v", models)
	}
}

func TestGetMentalModelsDisabled(t *testing.T) {
	c := NewClient(WithEnabled(false))
	models, err := c.GetMentalModels(context.Background())
	if err != nil || models != nil {
		t.Fatalf("disabled client must be a nil no-op, got models=%v err=%v", models, err)
	}
}

func TestGetMentalModelsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithEnabled(true))
	if _, err := c.GetMentalModels(context.Background()); err == nil {
		t.Fatal("want error on 500, got nil")
	}
}
