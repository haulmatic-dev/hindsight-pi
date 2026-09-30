package core

import (
	"errors"
	"fmt"
	"testing"

	"github.com/haulmatic-dev/hydra/pkg/types"
)

func withTestPublishIdentityResolver(t *testing.T, fn func(string) (PublishIdentity, error)) {
	t.Helper()
	SetPublishIdentityResolver(fn)
	t.Cleanup(func() { SetPublishIdentityResolver(nil) })
}

func TestSpawnPreflightPublishIdentity(t *testing.T) {
	bead := &types.Bead{ID: types.BeadID("proj-1"), Project: "proj"}

	t.Run("nil resolver never gates", func(t *testing.T) {
		SetPublishIdentityResolver(nil)
		o := &Overseer{}
		if err := o.spawnPreflightPublishIdentity(bead, "/tmp/proj"); err != nil {
			t.Fatalf("nil resolver must fail open, got %v", err)
		}
	})

	t.Run("resolved identity passes", func(t *testing.T) {
		withTestPublishIdentityResolver(t, func(string) (PublishIdentity, error) {
			return PublishIdentity{Owner: "alice", Token: "t", Source: "owner-pat-file"}, nil
		})
		o := &Overseer{}
		if err := o.spawnPreflightPublishIdentity(bead, "/tmp/proj"); err != nil {
			t.Fatalf("resolved identity must pass, got %v", err)
		}
	})

	t.Run("missing identity blocks with ErrPublishIdentityMissing", func(t *testing.T) {
		withTestPublishIdentityResolver(t, func(string) (PublishIdentity, error) {
			return PublishIdentity{}, fmt.Errorf("wrap: %w", ErrPublishIdentityMissing)
		})
		o := &Overseer{}
		err := o.spawnPreflightPublishIdentity(bead, "/tmp/proj")
		if !errors.Is(err, ErrPublishIdentityMissing) {
			t.Fatalf("want ErrPublishIdentityMissing, got %v", err)
		}
	})

	t.Run("non-identity resolver error fails open", func(t *testing.T) {
		withTestPublishIdentityResolver(t, func(string) (PublishIdentity, error) {
			return PublishIdentity{}, errors.New("transient userdb outage")
		})
		o := &Overseer{}
		if err := o.spawnPreflightPublishIdentity(bead, "/tmp/proj"); err != nil {
			t.Fatalf("transient resolver error must fail open, got %v", err)
		}
	})

	t.Run("unknown project fails open", func(t *testing.T) {
		withTestPublishIdentityResolver(t, func(string) (PublishIdentity, error) {
			return PublishIdentity{}, fmt.Errorf("wrap: %w", ErrPublishIdentityMissing)
		})
		o := &Overseer{} // no beadAnalyzer -> projectPath unresolvable -> pass
		if err := o.spawnPreflightPublishIdentity(bead, ""); err != nil {
			t.Fatalf("unresolvable project path must fail open, got %v", err)
		}
	})
}
