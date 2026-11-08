package core

import "testing"

func TestParseCgroupV2Path(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"unified v2", "0::/system.slice/hydra.service\n", "/system.slice/hydra.service"},
		{"v1 entries present, v2 line wins", "5:cpu,cpuacct:/user.slice\n0::/system.slice/hydra.service\n", "/system.slice/hydra.service"},
		{"v1 only", "5:cpu,cpuacct:/user.slice\n", ""},
		{"root cgroup", "0::/\n", "/"},
		{"empty", "", ""},
		{"trailing whitespace", "0::/system.slice/hydra.service  \n", "/system.slice/hydra.service"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseCgroupV2Path(tc.content); got != tc.want {
				t.Errorf("parseCgroupV2Path(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}

func TestConfineWorkerToCgroupDisabled(t *testing.T) {
	// maxMB <= 0 must be a no-op even with a bogus pid — proves the
	// fail-open path never blocks spawning.
	s := &WorkerSpawner{config: &Config{}}
	s.confineWorkerToCgroup(99999999, "test-bead") // must not panic or block
}
