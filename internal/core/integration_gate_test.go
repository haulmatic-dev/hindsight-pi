package core

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func withRepoCooldown(t *testing.T, d time.Duration) {
	t.Helper()
	old := integrationRepoCooldown
	integrationRepoCooldown = d
	t.Cleanup(func() { integrationRepoCooldown = old })
}

func TestIntegrationGate_SerializesPerRepo(t *testing.T) {
	withRepoCooldown(t, 0)
	resetIntegrationCooldown("repo-a")
	var cur, max atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = integrationGate("repo-a", func() error {
				n := cur.Add(1)
				for {
					m := max.Load()
					if n <= m || max.CompareAndSwap(m, n) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				cur.Add(-1)
				return nil
			})
		}()
	}
	wg.Wait()
	if m := max.Load(); m != 1 {
		t.Fatalf("max concurrent gated integrations = %d, want 1", m)
	}
}

func TestIntegrationGate_ReposIndependent(t *testing.T) {
	withRepoCooldown(t, 0)
	var mu sync.Mutex
	inside := map[string]int{}
	max := map[string]int{}
	var wg sync.WaitGroup
	for _, repo := range []string{"repo-x", "repo-y"} {
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(repo string) {
				defer wg.Done()
				_ = integrationGate(repo, func() error {
					mu.Lock()
					inside[repo]++
					if inside[repo] > max[repo] {
						max[repo] = inside[repo]
					}
					mu.Unlock()
					time.Sleep(2 * time.Millisecond)
					mu.Lock()
					inside[repo] = 0
					mu.Unlock()
					return nil
				})
			}(repo)
		}
	}
	wg.Wait()
	for repo, m := range max {
		if m != 1 {
			t.Fatalf("repo %s: max concurrency %d, want 1", repo, m)
		}
	}
}

func TestIntegrationGate_CooldownAfterFailure(t *testing.T) {
	withRepoCooldown(t, 80*time.Millisecond)
	resetIntegrationCooldown("repo-c")
	boom := errors.New("boom")
	if err := integrationGate("repo-c", func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("first gate err = %v", err)
	}
	start := time.Now()
	called := false
	_ = integrationGate("repo-c", func() error { called = true; return nil })
	if !called {
		t.Fatal("second gate fn never ran")
	}
	if waited := time.Since(start); waited < 80*time.Millisecond {
		t.Fatalf("second gate ran after %v, want >= cooldown 80ms", waited)
	}
}

func TestIntegrationGate_EscalatingCooldown(t *testing.T) {
	withRepoCooldown(t, 40*time.Millisecond)
	resetIntegrationCooldown("repo-e")
	boom := errors.New("boom")
	if err := integrationGate("repo-e", func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("fail 1 err = %v", err)
	}
	start := time.Now()
	_ = integrationGate("repo-e", func() error { return boom })
	if w := time.Since(start); w < 40*time.Millisecond {
		t.Fatalf("after 1 failure gate waited %v, want >= base 40ms", w)
	}
	start = time.Now()
	_ = integrationGate("repo-e", func() error { return boom })
	if w := time.Since(start); w < 80*time.Millisecond {
		t.Fatalf("after 2 failures gate waited %v, want >= 2x base 80ms", w)
	}
	start = time.Now()
	_ = integrationGate("repo-e", func() error { return boom })
	if w := time.Since(start); w < 160*time.Millisecond {
		t.Fatalf("after 3 failures gate waited %v, want >= 4x base 160ms", w)
	}
	// Streak must not leak across repos: repo-e is at streak 3 (next
	// cooldown 160ms); repo-f's next wait after one failure is only base 40ms.
	resetIntegrationCooldown("repo-f")
	_ = integrationGate("repo-f", func() error { return boom })
	start = time.Now()
	_ = integrationGate("repo-f", func() error { return nil })
	if w := time.Since(start); w >= 160*time.Millisecond {
		t.Fatalf("fresh repo waited %v after one failure, want < escalated 160ms (other repo's streak not shared)", w)
	}
}

func TestIntegrationGate_SuccessResetsStreak(t *testing.T) {
	withRepoCooldown(t, 40*time.Millisecond)
	resetIntegrationCooldown("repo-g")
	boom := errors.New("boom")
	_ = integrationGate("repo-g", func() error { return boom })
	_ = integrationGate("repo-g", func() error { return boom })
	_ = integrationGate("repo-g", func() error { return nil })
	start := time.Now()
	_ = integrationGate("repo-g", func() error { return boom })
	if w := time.Since(start); w > 20*time.Millisecond {
		t.Fatalf("first gate after success waited %v, want immediate", w)
	}
	if v, ok := integrationRepoFailStreak.Load("repo-g"); !ok || v.(int) != 1 {
		t.Fatalf("streak after success+failure = %v, want 1", v)
	}
}

func TestIntegrationGate_SuccessClearsCooldown(t *testing.T) {
	withRepoCooldown(t, 80*time.Millisecond)
	resetIntegrationCooldown("repo-d")
	_ = integrationGate("repo-d", func() error { return errors.New("fail once") })
	resetIntegrationCooldown("repo-d")
	start := time.Now()
	_ = integrationGate("repo-d", func() error { return nil })
	if waited := time.Since(start); waited > 40*time.Millisecond {
		t.Fatalf("post-reset gate waited %v, want immediate", waited)
	}
}
