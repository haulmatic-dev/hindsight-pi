package core

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// ConfigChangeHandler is called when the config file changes
type ConfigChangeHandler func(oldConfig, newConfig *Config) error

// FileWatcher monitors a file for changes
type FileWatcher interface {
	Start() error
	Stop() error
	IsRunning() bool
}

// fileState tracks the current state of a watched file
type fileState struct {
	modTime time.Time
	size    int64
	content []byte
}

// PollingFileWatcher watches a file by periodically checking for changes
type PollingFileWatcher struct {
	filePath      string
	interval      time.Duration
	onChange      func() error
	stopChan      chan struct{}
	stopOnce      sync.Once
	isRunning     bool
	mu            sync.RWMutex
	lastState     fileState
	debounceTimer *time.Timer
	debounceDelay time.Duration
}

// PollingFileWatcherOption configures the polling file watcher
type PollingFileWatcherOption func(*PollingFileWatcher)

// WithPollingInterval sets the polling interval
func WithPollingInterval(interval time.Duration) PollingFileWatcherOption {
	return func(w *PollingFileWatcher) {
		w.interval = interval
	}
}

// WithDebounceDelay sets the debounce delay for change detection
func WithDebounceDelay(delay time.Duration) PollingFileWatcherOption {
	return func(w *PollingFileWatcher) {
		w.debounceDelay = delay
	}
}

// NewPollingFileWatcher creates a new polling-based file watcher
func NewPollingFileWatcher(filePath string, onChange func() error, opts ...PollingFileWatcherOption) (*PollingFileWatcher, error) {
	if filePath == "" {
		return nil, fmt.Errorf("file path cannot be empty")
	}
	if onChange == nil {
		return nil, fmt.Errorf("onChange handler cannot be nil")
	}

	watcher := &PollingFileWatcher{
		filePath:      filePath,
		onChange:      onChange,
		interval:      5 * time.Second,
		stopChan:      make(chan struct{}),
		debounceDelay: 100 * time.Millisecond,
	}

	for _, opt := range opts {
		opt(watcher)
	}

	return watcher, nil
}

// Start begins watching the file for changes
func (w *PollingFileWatcher) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.isRunning {
		return fmt.Errorf("watcher already running")
	}

	// Capture initial state
	state, err := w.readFileState()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read initial file state: %w", err)
	}
	w.lastState = state
	w.isRunning = true

	go w.watchLoop()
	return nil
}

// Stop halts the file watcher
func (w *PollingFileWatcher) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.isRunning {
		return nil
	}

	w.stopOnce.Do(func() {
		close(w.stopChan)
	})

	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
	}

	w.isRunning = false
	return nil
}

// IsRunning returns whether the watcher is currently running
func (w *PollingFileWatcher) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.isRunning
}

// watchLoop is the main polling loop
func (w *PollingFileWatcher) watchLoop() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			if changed := w.detectChange(); changed {
				w.handleChange()
			}
		}
	}
}

// readFileState captures the current state of the file
func (w *PollingFileWatcher) readFileState() (fileState, error) {
	info, err := os.Stat(w.filePath)
	if err != nil {
		return fileState{}, err
	}

	content, err := os.ReadFile(w.filePath)
	if err != nil {
		return fileState{}, err
	}

	return fileState{
		modTime: info.ModTime(),
		size:    info.Size(),
		content: content,
	}, nil
}

// detectChange checks if the file has changed
func (w *PollingFileWatcher) detectChange() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	currentState, err := w.readFileState()
	if err != nil {
		// File might not exist temporarily, ignore
		return false
	}

	// Compare modification time and size first (fast check)
	if currentState.modTime.Equal(w.lastState.modTime) && currentState.size == w.lastState.size {
		return false
	}

	// Compare content (slower but accurate)
	if string(currentState.content) == string(w.lastState.content) {
		return false
	}

	w.lastState = currentState
	return true
}

// handleChange processes a detected change with debouncing
func (w *PollingFileWatcher) handleChange() {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Cancel any pending debounce timer
	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
	}

	// Start new debounce timer
	w.debounceTimer = time.AfterFunc(w.debounceDelay, func() {
		if err := w.onChange(); err != nil {
			// Log error but don't stop watching
			fmt.Printf("Config change handler error: %v\n", err)
		}
	})
}

// ConfigWatcher watches the Hydra configuration file for changes
type ConfigWatcher struct {
	fileWatcher FileWatcher
	configPath  string
	onChange    ConfigChangeHandler
	currentCfg  *Config
	mu          sync.RWMutex
}

// ConfigWatcherOption configures the config watcher
type ConfigWatcherOption func(*ConfigWatcher)

// WithFileWatcher sets a custom file watcher implementation
func WithFileWatcher(watcher FileWatcher) ConfigWatcherOption {
	return func(cw *ConfigWatcher) {
		cw.fileWatcher = watcher
	}
}

// NewConfigWatcher creates a new configuration file watcher
func NewConfigWatcher(configPath string, onChange ConfigChangeHandler, opts ...ConfigWatcherOption) (*ConfigWatcher, error) {
	if configPath == "" {
		return nil, fmt.Errorf("config path cannot be empty")
	}
	if onChange == nil {
		return nil, fmt.Errorf("change handler cannot be nil")
	}

	cw := &ConfigWatcher{
		configPath: configPath,
		onChange:   onChange,
	}

	for _, opt := range opts {
		opt(cw)
	}

	// Create default file watcher if not provided
	if cw.fileWatcher == nil {
		fw, err := NewPollingFileWatcher(configPath, cw.handleFileChange)
		if err != nil {
			return nil, fmt.Errorf("failed to create file watcher: %w", err)
		}
		cw.fileWatcher = fw
	}

	return cw, nil
}

// Start begins watching the configuration file
func (cw *ConfigWatcher) Start() error {
	// Load initial config
	cfg, err := LoadConfig(cw.configPath)
	if err != nil {
		return fmt.Errorf("failed to load initial config: %w", err)
	}

	cw.mu.Lock()
	cw.currentCfg = cfg
	cw.mu.Unlock()

	return cw.fileWatcher.Start()
}

// Stop halts the configuration watcher
func (cw *ConfigWatcher) Stop() error {
	return cw.fileWatcher.Stop()
}

// IsRunning returns whether the watcher is running
func (cw *ConfigWatcher) IsRunning() bool {
	return cw.fileWatcher.IsRunning()
}

// CurrentConfig returns the current configuration
func (cw *ConfigWatcher) CurrentConfig() *Config {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	if cw.currentCfg == nil {
		return nil
	}
	// Return a copy to prevent external modification
	return cw.currentCfg.Clone()
}

// handleFileChange is called when the file watcher detects a change
func (cw *ConfigWatcher) handleFileChange() error {
	// Load new config
	newConfig, err := LoadConfig(cw.configPath)
	if err != nil {
		return fmt.Errorf("failed to load config on change: %w", err)
	}

	// Validate the new config
	if err := ValidateConfigSingleError(newConfig); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	cw.mu.Lock()
	oldConfig := cw.currentCfg
	cw.currentCfg = newConfig
	cw.mu.Unlock()

	// Notify handler
	if err := cw.onChange(oldConfig, newConfig); err != nil {
		return fmt.Errorf("change handler failed: %w", err)
	}

	return nil
}
