package pi

import (
	"testing"
	"time"
)

func TestNewSkillUpdater(t *testing.T) {
	setupManager := NewSetupManager("./test-config", "./test-config")

	tests := []struct {
		name         string
		intervalStr  string
		wantErr      bool
		wantInterval time.Duration
	}{
		{
			name:         "valid 24h interval",
			intervalStr:  "24h",
			wantErr:      false,
			wantInterval: 24 * time.Hour,
		},
		{
			name:         "valid 1h interval",
			intervalStr:  "1h",
			wantErr:      false,
			wantInterval: 1 * time.Hour,
		},
		{
			name:         "valid 30m interval",
			intervalStr:  "30m",
			wantErr:      false,
			wantInterval: 30 * time.Minute,
		},
		{
			name:        "invalid interval",
			intervalStr: "invalid",
			wantErr:     true,
		},
		{
			name:        "zero interval",
			intervalStr: "0s",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updater, err := NewSkillUpdater(setupManager, tt.intervalStr)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewSkillUpdater() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("NewSkillUpdater() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if updater == nil {
				t.Error("NewSkillUpdater() returned nil updater")
				return
			}
			if updater.GetUpdateInterval() != tt.wantInterval {
				t.Errorf("GetUpdateInterval() = %v, want %v", updater.GetUpdateInterval(), tt.wantInterval)
			}
		})
	}
}

func TestSkillUpdater_StartStop(t *testing.T) {
	setupManager := NewSetupManager("./test-config", "./test-config")
	updater, err := NewSkillUpdater(setupManager, "24h")
	if err != nil {
		t.Fatalf("NewSkillUpdater() error = %v", err)
	}

	// Test start
	updater.Start()
	if !updater.IsRunning() {
		t.Error("Expected updater to be running after Start()")
	}

	// Test double start (should not panic or create duplicate goroutines)
	updater.Start()

	// Test stop
	updater.Stop()
	if updater.IsRunning() {
		t.Error("Expected updater to not be running after Stop()")
	}

	// Test double stop (should not panic)
	updater.Stop()
}

func TestSkillUpdater_UpdateSkillsNow_NotRunning(t *testing.T) {
	setupManager := NewSetupManager("./test-config", "./test-config")
	updater, err := NewSkillUpdater(setupManager, "24h")
	if err != nil {
		t.Fatalf("NewSkillUpdater() error = %v", err)
	}

	// Should return error if updater is not running
	err = updater.UpdateSkillsNow()
	if err == nil {
		t.Error("UpdateSkillsNow() should return error when updater is not running")
	}
}
