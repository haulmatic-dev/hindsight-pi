package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBeadID_String(t *testing.T) {
	tests := []struct {
		name     string
		beadID   BeadID
		expected string
	}{
		{
			name:     "single-repo format",
			beadID:   BeadID("hydra-zxc"),
			expected: "hydra-zxc",
		},
		{
			name:     "multi-repo format",
			beadID:   BeadID("hydra/backend-abc123"),
			expected: "hydra/backend-abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.beadID.String(); got != tt.expected {
				t.Errorf("BeadID.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBeadID_Project(t *testing.T) {
	tests := []struct {
		name     string
		beadID   BeadID
		expected string
	}{
		{
			name:     "single-repo",
			beadID:   BeadID("myproject-abc"),
			expected: "myproject",
		},
		{
			name:     "multi-repo",
			beadID:   BeadID("myproject/backend-abc"),
			expected: "myproject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.beadID.Project(); got != tt.expected {
				t.Errorf("BeadID.Project() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBeadID_Repo(t *testing.T) {
	tests := []struct {
		name     string
		beadID   BeadID
		expected string
	}{
		{
			name:     "single-repo",
			beadID:   BeadID("myproject-abc"),
			expected: "",
		},
		{
			name:     "multi-repo",
			beadID:   BeadID("myproject/backend-abc"),
			expected: "backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.beadID.Repo(); got != tt.expected {
				t.Errorf("BeadID.Repo() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBeadID_ID(t *testing.T) {
	tests := []struct {
		name     string
		beadID   BeadID
		expected string
	}{
		{
			name:     "single-repo",
			beadID:   BeadID("myproject-abc123"),
			expected: "abc123",
		},
		{
			name:     "multi-repo",
			beadID:   BeadID("myproject/backend-abc123"),
			expected: "abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.beadID.ID(); got != tt.expected {
				t.Errorf("BeadID.ID() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBeadID_Number(t *testing.T) {
	tests := []struct {
		name        string
		beadID      BeadID
		expected    int
		expectError bool
	}{
		{
			name:        "numeric ID",
			beadID:      BeadID("myproject-123"),
			expected:    123,
			expectError: false,
		},
		{
			name:        "base36 ID",
			beadID:      BeadID("myproject-abc"),
			expected:    0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.beadID.Number()
			if (err != nil) != tt.expectError {
				t.Errorf("BeadID.Number() error = %v, expectError %v", err, tt.expectError)
				return
			}
			if got != tt.expected {
				t.Errorf("BeadID.Number() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseBeadID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    BeadID
		expectError bool
	}{
		{
			name:        "valid single-repo",
			input:       "hydra-zxc",
			expected:    BeadID("hydra-zxc"),
			expectError: false,
		},
		{
			name:        "valid multi-repo",
			input:       "hydra/backend-abc",
			expected:    BeadID("hydra/backend-abc"),
			expectError: false,
		},
		{
			name:        "empty string",
			input:       "",
			expectError: true,
		},
		{
			name:        "no dash separator",
			input:       "invalidid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBeadID(tt.input)
			if (err != nil) != tt.expectError {
				t.Errorf("ParseBeadID() error = %v, expectError %v", err, tt.expectError)
				return
			}
			if !tt.expectError && got != tt.expected {
				t.Errorf("ParseBeadID() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNewBeadID(t *testing.T) {
	tests := []struct {
		name     string
		project  string
		repo     string
		id       string
		expected BeadID
	}{
		{
			name:     "single-repo",
			project:  "hydra",
			repo:     "",
			id:       "abc",
			expected: BeadID("hydra-abc"),
		},
		{
			name:     "multi-repo",
			project:  "hydra",
			repo:     "backend",
			id:       "abc",
			expected: BeadID("hydra/backend-abc"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewBeadID(tt.project, tt.repo, tt.id)
			if got != tt.expected {
				t.Errorf("NewBeadID() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBeadID_MarshalJSON(t *testing.T) {
	beadID := BeadID("hydra-zxc")
	data, err := json.Marshal(beadID)
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	expected := `"hydra-zxc"`
	if string(data) != expected {
		t.Errorf("MarshalJSON() = %v, want %v", string(data), expected)
	}
}

func TestBeadID_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    BeadID
		expectError bool
	}{
		{
			name:        "valid",
			input:       `"hydra-zxc"`,
			expected:    BeadID("hydra-zxc"),
			expectError: false,
		},
		{
			name:        "invalid format",
			input:       `"invalidid"`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got BeadID
			err := json.Unmarshal([]byte(tt.input), &got)
			if (err != nil) != tt.expectError {
				t.Errorf("UnmarshalJSON() error = %v, expectError %v", err, tt.expectError)
				return
			}
			if !tt.expectError && got != tt.expected {
				t.Errorf("UnmarshalJSON() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDetermineAgentTypes(t *testing.T) {
	tests := []struct {
		beadType string
		expected []AgentType
	}{
		{
			beadType: "implement",
			expected: []AgentType{AgentOrchestrator},
		},
		{
			beadType: "fix",
			expected: []AgentType{AgentFixSpecialist},
		},
		{
			beadType: "unknown",
			expected: []AgentType{AgentOrchestrator},
		},
	}

	for _, tt := range tests {
		t.Run(tt.beadType, func(t *testing.T) {
			got := DetermineAgentTypes(tt.beadType)
			if len(got) != len(tt.expected) {
				t.Errorf("DetermineAgentTypes() = %v, want %v", got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("DetermineAgentTypes()[%d] = %v, want %v", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestBeadStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status   BeadStatus
		expected bool
	}{
		{StatusCompleted, true},
		{StatusFailed, true},
		{StatusOpen, false},
		{StatusInProgress, false},
		{StatusBlocked, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.expected {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLegacyBeadID_Conversion(t *testing.T) {
	// Test legacy to new
	legacy := LegacyBeadID{
		Project: "hydra",
		Repo:    "backend",
		Number:  123,
	}

	beadID := legacy.ToBeadID()
	expected := BeadID("hydra/backend-123")
	if beadID != expected {
		t.Errorf("ToBeadID() = %v, want %v", beadID, expected)
	}

	// Test new to legacy
	newBeadID := BeadID("hydra/backend-456")
	legacy2, err := LegacyBeadIDFromBeadID(newBeadID)
	if err != nil {
		t.Fatalf("LegacyBeadIDFromBeadID() error = %v", err)
	}

	if legacy2.Project != "hydra" || legacy2.Repo != "backend" || legacy2.Number != 456 {
		t.Errorf("LegacyBeadIDFromBeadID() = %+v, want Project=hydra, Repo=backend, Number=456", legacy2)
	}
}

func TestBead_JSON(t *testing.T) {
	createdAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	bead := Bead{
		ID:           BeadID("hydra-abc"),
		Title:        "Test Bead",
		Description:  "Test Description",
		Type:         "implement",
		Status:       StatusOpen,
		Dependencies: []BeadID{BeadID("hydra-dep1")},
		Project:      "hydra",
		Repo:         "",
		Labels:       []string{"test", "important"},
		Priority:     5,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
		Metadata:     map[string]string{"key": "value"},
	}

	// Marshal to JSON
	data, err := json.Marshal(bead)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Unmarshal back
	var decoded Bead
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Verify fields
	if decoded.ID != bead.ID {
		t.Errorf("ID mismatch: got %v, want %v", decoded.ID, bead.ID)
	}
	if decoded.Title != bead.Title {
		t.Errorf("Title mismatch: got %v, want %v", decoded.Title, bead.Title)
	}
	if len(decoded.Dependencies) != len(bead.Dependencies) {
		t.Errorf("Dependencies length mismatch: got %d, want %d", len(decoded.Dependencies), len(bead.Dependencies))
	}
}
