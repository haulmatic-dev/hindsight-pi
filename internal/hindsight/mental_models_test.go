package hindsight

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetMentalModelsFiltersEmptyContent(t *testing.T) {
	// LIST with ?detail=content returns content inline (live-verified on
	// 1.2.0); never-refreshed shells carry empty content and must be filtered.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/default/banks/coding-brain/mental-models" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("detail") != "content" {
			t.Errorf("expected detail=content param, got %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"items":[
			{"id":"mm-1","name":"Has content","content":"rule A"},
			{"id":"mm-2","name":"Never refreshed","content":""}
		],"total":2,"limit":100,"offset":0}`))
	}))
	defer srv.Close()

	c := NewClient(WithBaseURL(srv.URL), WithEnabled(true))
	models, err := c.GetMentalModels(context.Background())
	if err != nil {
		t.Fatalf("GetMentalModels: %v", err)
	}
	if len(models) != 1 || models[0].Name != "Has content" || models[0].Content != "rule A" {
		t.Fatalf("unexpected models: %+v", models)
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
