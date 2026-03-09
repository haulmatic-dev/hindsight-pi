package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

var (
	// ErrFileReserved is returned when a file is already reserved by another agent
	ErrFileReserved = errors.New("file is already reserved")

	// ErrReservationNotFound is returned when a reservation is not found
	ErrReservationNotFound = errors.New("reservation not found")

	// ErrReservationExpired is returned when a reservation has expired
	ErrReservationExpired = errors.New("reservation has expired")
)

// ReservationConflictError provides details about a reservation conflict
type ReservationConflictError struct {
	File       string
	ReservedBy string
	BeadID     string
	ExpiresAt  time.Time
}

func (e *ReservationConflictError) Error() string {
	return fmt.Sprintf("file %q is reserved by agent %q (bead: %s, expires: %s)",
		e.File, e.ReservedBy, e.BeadID, e.ExpiresAt.Format(time.RFC3339))
}

// Reservation represents a file reservation held by an agent
type Reservation struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	BeadID    string    `json:"bead_id"`
	Files     []string  `json:"files"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// IsExpired returns true if the reservation has expired
func (r *Reservation) IsExpired() bool {
	return time.Now().After(r.ExpiresAt)
}

// RemainingTime returns the remaining time until expiration
func (r *Reservation) RemainingTime() time.Duration {
	if r.IsExpired() {
		return 0
	}
	return time.Until(r.ExpiresAt)
}

// ContainsFile checks if the reservation includes a specific file
func (r *Reservation) ContainsFile(file string) bool {
	for _, f := range r.Files {
		if f == file {
			return true
		}
	}
	return false
}

// ReservationRequest represents a request to reserve files
type ReservationRequest struct {
	Files  []string      `json:"files"`
	TTL    time.Duration `json:"ttl"`
	BeadID string        `json:"bead_id"`
}

// ReservationResponse represents a response from the reservation server
type ReservationResponse struct {
	Reservation *Reservation          `json:"reservation,omitempty"`
	Success     bool                  `json:"success"`
	Error       string                `json:"error,omitempty"`
	Conflicts   []ReservationConflict `json:"conflicts,omitempty"`
}

// ReservationConflict represents a single file conflict in a batch reservation
type ReservationConflict struct {
	File       string    `json:"file"`
	ReservedBy string    `json:"reserved_by"`
	BeadID     string    `json:"bead_id"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// ReservationManager manages local reservation cache with thread-safe operations
type ReservationManager struct {
	reservations map[string]*Reservation // key: file path
	mu           sync.RWMutex
}

// NewReservationManager creates a new ReservationManager
func NewReservationManager() *ReservationManager {
	return &ReservationManager{
		reservations: make(map[string]*Reservation),
	}
}

// Add adds a reservation to the manager for all files in the reservation
func (rm *ReservationManager) Add(res *Reservation) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for _, file := range res.Files {
		rm.reservations[file] = res
	}
}

// Remove removes a reservation for a specific file
func (rm *ReservationManager) Remove(file string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	delete(rm.reservations, file)
}

// RemoveReservation removes all entries for a specific reservation ID
func (rm *ReservationManager) RemoveReservation(reservationID string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for file, res := range rm.reservations {
		if res.ID == reservationID {
			delete(rm.reservations, file)
		}
	}
}

// Get retrieves a reservation for a specific file
func (rm *ReservationManager) Get(file string) *Reservation {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	res := rm.reservations[file]
	if res != nil && res.IsExpired() {
		return nil
	}
	return res
}

// GetByReservationID retrieves all files for a specific reservation ID
func (rm *ReservationManager) GetByReservationID(reservationID string) []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var files []string
	for file, res := range rm.reservations {
		if res.ID == reservationID && !res.IsExpired() {
			files = append(files, file)
		}
	}
	return files
}

// CleanupExpired removes all expired reservations
func (rm *ReservationManager) CleanupExpired() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for file, res := range rm.reservations {
		if res.IsExpired() {
			delete(rm.reservations, file)
		}
	}
}

// GetAllReservations returns a copy of all active reservations
func (rm *ReservationManager) GetAllReservations() map[string]*Reservation {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := make(map[string]*Reservation)
	for file, res := range rm.reservations {
		if !res.IsExpired() {
			result[file] = res
		}
	}
	return result
}

// ReservationClient handles HTTP-based file reservations with the MCP server
type ReservationClient struct {
	agentID            string
	baseURL            string
	httpClient         *http.Client
	ctx                context.Context
	timeout            time.Duration
	reservationManager *ReservationManager
}

// NewReservationClient creates a new reservation client
func NewReservationClient(agentID, baseURL string, timeout time.Duration) *ReservationClient {
	return &ReservationClient{
		agentID:            agentID,
		baseURL:            baseURL,
		httpClient:         &http.Client{Timeout: timeout},
		ctx:                context.Background(),
		timeout:            timeout,
		reservationManager: NewReservationManager(),
	}
}

// NewReservationClientWithContext creates a new client with custom context
func NewReservationClientWithContext(ctx context.Context, agentID, baseURL string, timeout time.Duration) *ReservationClient {
	return &ReservationClient{
		agentID:            agentID,
		baseURL:            baseURL,
		httpClient:         &http.Client{Timeout: timeout},
		ctx:                ctx,
		timeout:            timeout,
		reservationManager: NewReservationManager(),
	}
}

// SetHTTPClient allows setting a custom HTTP client
func (rc *ReservationClient) SetHTTPClient(client *http.Client) {
	rc.httpClient = client
}

// generateID generates a unique reservation ID
func generateID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Nanosecond())
}

// ReserveFiles attempts to reserve the specified files with the given TTL
func (rc *ReservationClient) ReserveFiles(files []string, ttl time.Duration, beadID string) (*Reservation, error) {
	if len(files) == 0 {
		return nil, errors.New("no files specified for reservation")
	}

	if ttl <= 0 {
		return nil, errors.New("TTL must be positive")
	}

	ctx, cancel := context.WithTimeout(rc.ctx, rc.timeout)
	defer cancel()

	req := ReservationRequest{
		Files:  files,
		TTL:    ttl,
		BeadID: beadID,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal reservation request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		rc.baseURL+"/reservations", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create reservation request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Agent-ID", rc.agentID)

	resp, err := rc.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute reservation request: %w", err)
	}
	defer resp.Body.Close()

	var resResp ReservationResponse
	if err := json.NewDecoder(resp.Body).Decode(&resResp); err != nil {
		return nil, fmt.Errorf("failed to decode reservation response: %w", err)
	}

	if resp.StatusCode == http.StatusConflict {
		if len(resResp.Conflicts) > 0 {
			conflict := resResp.Conflicts[0]
			return nil, &ReservationConflictError{
				File:       conflict.File,
				ReservedBy: conflict.ReservedBy,
				BeadID:     conflict.BeadID,
				ExpiresAt:  conflict.ExpiresAt,
			}
		}
		return nil, ErrFileReserved
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		if resResp.Error != "" {
			return nil, fmt.Errorf("reservation failed: %s", resResp.Error)
		}
		return nil, fmt.Errorf("reservation failed with status: %d", resp.StatusCode)
	}

	if resResp.Reservation == nil {
		return nil, errors.New("reservation response missing reservation data")
	}

	// Update local cache
	rc.reservationManager.Add(resResp.Reservation)

	return resResp.Reservation, nil
}

// ReleaseFiles releases the reservation for the specified files
func (rc *ReservationClient) ReleaseFiles(files []string) error {
	if len(files) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(rc.ctx, rc.timeout)
	defer cancel()

	body, err := json.Marshal(map[string][]string{"files": files})
	if err != nil {
		return fmt.Errorf("failed to marshal release request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		rc.baseURL+"/reservations", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create release request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Agent-ID", rc.agentID)

	resp, err := rc.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to execute release request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != "" {
			return fmt.Errorf("failed to release files: %s", errResp.Error)
		}
		return fmt.Errorf("failed to release files, status: %d", resp.StatusCode)
	}

	// Update local cache
	for _, file := range files {
		rc.reservationManager.Remove(file)
	}

	return nil
}

// CheckReservation checks if a file is reserved and returns the reservation details
func (rc *ReservationClient) CheckReservation(file string) (*Reservation, error) {
	// First check local cache
	if res := rc.reservationManager.Get(file); res != nil {
		return res, nil
	}

	ctx, cancel := context.WithTimeout(rc.ctx, rc.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		rc.baseURL+"/reservations/check?file="+url.QueryEscape(file), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create check request: %w", err)
	}

	resp, err := rc.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute check request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrReservationNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("check reservation failed with status: %d", resp.StatusCode)
	}

	var res Reservation
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode reservation: %w", err)
	}

	if res.IsExpired() {
		return nil, ErrReservationExpired
	}

	// Update local cache
	rc.reservationManager.Add(&res)

	return &res, nil
}

// ExtendReservation extends the expiration time of existing reservations
func (rc *ReservationClient) ExtendReservation(files []string, additionalTTL time.Duration) error {
	if len(files) == 0 {
		return nil
	}

	if additionalTTL <= 0 {
		return errors.New("additional TTL must be positive")
	}

	ctx, cancel := context.WithTimeout(rc.ctx, rc.timeout)
	defer cancel()

	reqBody := map[string]interface{}{
		"files":          files,
		"additional_ttl": additionalTTL.String(),
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal extend request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch,
		rc.baseURL+"/reservations", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create extend request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Agent-ID", rc.agentID)

	resp, err := rc.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to execute extend request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrReservationNotFound
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != "" {
			return fmt.Errorf("failed to extend reservation: %s", errResp.Error)
		}
		return fmt.Errorf("failed to extend reservation, status: %d", resp.StatusCode)
	}

	var updatedRes Reservation
	if err := json.NewDecoder(resp.Body).Decode(&updatedRes); err != nil {
		// Update cache optimistically
		for _, file := range files {
			if res := rc.reservationManager.Get(file); res != nil {
				res.ExpiresAt = time.Now().Add(additionalTTL)
			}
		}
		return nil
	}

	rc.reservationManager.Add(&updatedRes)
	return nil
}

// ValidateReservation checks if the current agent can modify the specified file
func (rc *ReservationClient) ValidateReservation(file string) error {
	res, err := rc.CheckReservation(file)
	if err != nil {
		if errors.Is(err, ErrReservationNotFound) {
			return nil // No reservation means free to edit
		}
		return err
	}

	if res.AgentID == rc.agentID {
		return nil // File is reserved by current agent
	}

	return &ReservationConflictError{
		File:       file,
		ReservedBy: res.AgentID,
		BeadID:     res.BeadID,
		ExpiresAt:  res.ExpiresAt,
	}
}

// GetActiveReservations returns all active reservations for the current agent
func (rc *ReservationClient) GetActiveReservations() []*Reservation {
	allRes := rc.reservationManager.GetAllReservations()

	resMap := make(map[string]*Reservation)
	for _, res := range allRes {
		if res.AgentID == rc.agentID {
			resMap[res.ID] = res
		}
	}

	result := make([]*Reservation, 0, len(resMap))
	for _, res := range resMap {
		result = append(result, res)
	}
	return result
}

// CreateLocalReservation creates a reservation locally without server communication
func (rc *ReservationClient) CreateLocalReservation(files []string, ttl time.Duration, beadID string) (*Reservation, error) {
	if len(files) == 0 {
		return nil, errors.New("no files specified for reservation")
	}

	if ttl <= 0 {
		return nil, errors.New("TTL must be positive")
	}

	// Check for conflicts in local cache
	for _, file := range files {
		if existing := rc.reservationManager.Get(file); existing != nil {
			if existing.AgentID != rc.agentID {
				return nil, &ReservationConflictError{
					File:       file,
					ReservedBy: existing.AgentID,
					BeadID:     existing.BeadID,
					ExpiresAt:  existing.ExpiresAt,
				}
			}
		}
	}

	res := &Reservation{
		ID:        generateID(),
		AgentID:   rc.agentID,
		BeadID:    beadID,
		Files:     files,
		ExpiresAt: time.Now().Add(ttl),
		CreatedAt: time.Now(),
	}

	rc.reservationManager.Add(res)
	return res, nil
}

// StartCleanupTask starts a background goroutine that periodically cleans up expired reservations
func (rc *ReservationClient) StartCleanupTask(interval time.Duration) func() {
	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				rc.reservationManager.CleanupExpired()
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	return func() {
		close(done)
	}
}
