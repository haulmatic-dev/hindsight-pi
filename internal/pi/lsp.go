package pi

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
)

// LSPPackage represents an LSP package to install
type LSPPackage struct {
	Name        string
	Command     string
	NPMName     string
	Description string
}

// LSPComponent represents the installation status of an LSP component
type LSPComponent struct {
	Name      string
	Command   string
	Installed bool
}

// RequiredLSPPackages defines the LSP packages that need to be installed
var RequiredLSPPackages = []LSPPackage{
	{
		Name:        "LSP CLI",
		Command:     "lsp",
		NPMName:     "@pi/lsp-cli",
		Description: "Pi LSP CLI tool",
	},
	{
		Name:        "gopls",
		Command:     "gopls",
		NPMName:     "@pi/gopls-wrapper",
		Description: "Go language server",
	},
	{
		Name:        "typescript-language-server",
		Command:     "typescript-language-server",
		NPMName:     "typescript-language-server",
		Description: "TypeScript language server",
	},
}

// CheckAndInstallLSP checks and installs required LSP packages
func (sm *SetupManager) CheckAndInstallLSP() error {
	var failedPackages []string

	for _, pkg := range RequiredLSPPackages {
		log.Printf("[PiSetup] Checking %s...", pkg.Name)

		if sm.isLSPPackageInstalled(pkg) {
			log.Printf("[PiSetup] ✅ %s already installed", pkg.Name)
			continue
		}

		log.Printf("[PiSetup] Installing %s...", pkg.Name)
		if err := sm.installLSPPackage(pkg); err != nil {
			log.Printf("[PiSetup] ❌ Failed to install %s: %v", pkg.Name, err)
			failedPackages = append(failedPackages, pkg.Name)
		} else {
			log.Printf("[PiSetup] ✅ %s installed successfully", pkg.Name)
		}
	}

	if len(failedPackages) > 0 {
		return fmt.Errorf("failed to install LSP packages: %v", failedPackages)
	}

	return nil
}

// isLSPPackageInstalled checks if an LSP package is installed
func (sm *SetupManager) isLSPPackageInstalled(pkg LSPPackage) bool {
	cmd := exec.Command("which", pkg.Command)
	if err := cmd.Run(); err == nil {
		return true
	}

	// Also check via npx
	if runtime.GOOS != "windows" {
		cmd = exec.Command("npx", "--yes", pkg.NPMName, "--version")
	} else {
		cmd = exec.Command("npx.cmd", "--yes", pkg.NPMName, "--version")
	}
	return cmd.Run() == nil
}

// installLSPPackage installs a single LSP package
func (sm *SetupManager) installLSPPackage(pkg LSPPackage) error {
	var cmd *exec.Cmd

	// Try global npm install first
	log.Printf("[PiSetup] Installing %s via npm...", pkg.NPMName)
	cmd = exec.Command("npm", "install", "-g", pkg.NPMName)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// If global install fails, log the error but don't fail immediately
		// The package might still be available via npx
		log.Printf("[PiSetup] Global install of %s failed, will try npx: %v", pkg.Name, string(output))

		// Verify it works with npx
		if runtime.GOOS != "windows" {
			cmd = exec.Command("npx", "--yes", pkg.NPMName, "--version")
		} else {
			cmd = exec.Command("npx.cmd", "--yes", pkg.NPMName, "--version")
		}
		output, err = cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to install %s: %w (output: %s)", pkg.Name, err, string(output))
		}
	}

	return nil
}

// IsLSPInstalled checks if all required LSP packages are installed
func (sm *SetupManager) IsLSPInstalled() bool {
	for _, pkg := range RequiredLSPPackages {
		if !sm.isLSPPackageInstalled(pkg) {
			return false
		}
	}
	return true
}

// GetInstalledLSPPackages returns a list of installed LSP packages
func (sm *SetupManager) GetInstalledLSPPackages() []string {
	var installed []string
	for _, pkg := range RequiredLSPPackages {
		if sm.isLSPPackageInstalled(pkg) {
			installed = append(installed, pkg.Name)
		}
	}
	return installed
}

// GetMissingLSPPackages returns a list of missing LSP packages
func (sm *SetupManager) GetMissingLSPPackages() []string {
	var missing []string
	for _, pkg := range RequiredLSPPackages {
		if !sm.isLSPPackageInstalled(pkg) {
			missing = append(missing, pkg.Name)
		}
	}
	return missing
}

// CheckLSPInstallation checks all LSP components and returns detailed status for each
func (sm *SetupManager) CheckLSPInstallation() []LSPComponent {
	log.Println("[LSP] Checking LSP installation status...")

	components := []LSPComponent{
		{Name: "LSP CLI", Command: "lsp"},
		{Name: "gopls", Command: "gopls"},
		{Name: "typescript-language-server", Command: "typescript-language-server"},
	}

	for i := range components {
		cmd := exec.Command("which", components[i].Command)
		if err := cmd.Run(); err == nil {
			components[i].Installed = true
			log.Printf("[LSP] ✅ %s (%s) is installed", components[i].Name, components[i].Command)
		} else {
			components[i].Installed = false
			log.Printf("[LSP] ❌ %s (%s) is not installed", components[i].Name, components[i].Command)
		}
	}

	installed := 0
	for _, comp := range components {
		if comp.Installed {
			installed++
		}
	}
	log.Printf("[LSP] Installation check complete: %d/%d components installed", installed, len(components))

	return components
}

// InstallLSPComponents installs missing LSP components using appropriate package managers
// Verifies each installation succeeds and returns error if any installation fails
func (sm *SetupManager) InstallLSPComponents() error {
	var failedComponents []string

	log.Println("[LSP] Starting LSP component installation...")

	for _, pkg := range RequiredLSPPackages {
		// Skip if already installed
		if sm.isLSPPackageInstalled(pkg) {
			log.Printf("[LSP] ✅ %s already installed, skipping", pkg.Name)
			continue
		}

		log.Printf("[LSP] Installing %s...", pkg.Name)

		var cmd *exec.Cmd
		var output []byte
		var err error
		var installed bool

		// Use appropriate package manager based on component name
		switch pkg.Name {
		case "gopls":
			// Install gopls via go install
			log.Printf("[LSP] Installing gopls via 'go install'...")
			cmd = exec.Command("go", "install", "golang.org/x/tools/gopls@latest")
			output, err = cmd.CombinedOutput()
			if err != nil {
				log.Printf("[LSP] ❌ Failed to install gopls: %v\nOutput: %s", err, string(output))
				failedComponents = append(failedComponents, pkg.Name)
				continue
			}
			// Ensure the new binary is in PATH by refreshing shell config
			installed = true

		case "typescript-language-server":
			// Install typescript-language-server via npm
			log.Printf("[LSP] Installing typescript-language-server via npm...")
			cmd = exec.Command("npm", "install", "-g", "typescript-language-server")
			output, err = cmd.CombinedOutput()
			if err != nil {
				log.Printf("[LSP] ❌ Failed to install typescript-language-server: %v\nOutput: %s", err, string(output))
				failedComponents = append(failedComponents, pkg.Name)
				continue
			}
			installed = true

		case "LSP CLI":
			// Install lsp-client via pip (if needed) or npm
			// First try npm, then fallback to pip
			log.Printf("[LSP] Installing LSP CLI via npm...")
			cmd = exec.Command("npm", "install", "-g", pkg.NPMName)
			output, err = cmd.CombinedOutput()
			if err != nil {
				// Try pip as fallback for lsp-client
				log.Printf("[LSP] npm install failed, trying pip for lsp-client...")
				cmd = exec.Command("pip", "install", "lsp-client")
				output, err = cmd.CombinedOutput()
				if err != nil {
					log.Printf("[LSP] ❌ Failed to install LSP CLI via both npm and pip")
					failedComponents = append(failedComponents, pkg.Name)
					continue
				}
			}
			installed = true

		default:
			// Default to npm install for other packages
			log.Printf("[LSP] Installing %s via npm...", pkg.Name)
			cmd = exec.Command("npm", "install", "-g", pkg.NPMName)
			output, err = cmd.CombinedOutput()
			if err != nil {
				log.Printf("[LSP] ❌ Failed to install %s: %v\nOutput: %s", pkg.Name, err, string(output))
				failedComponents = append(failedComponents, pkg.Name)
				continue
			}
			installed = true
		}

		// Verify the installation succeeded
		if installed && !sm.isLSPPackageInstalled(pkg) {
			log.Printf("[LSP] ❌ Installation verification failed for %s", pkg.Name)
			failedComponents = append(failedComponents, pkg.Name)
			continue
		}

		log.Printf("[LSP] ✅ %s installed and verified successfully", pkg.Name)
	}

	if len(failedComponents) > 0 {
		return fmt.Errorf("failed to install LSP components: %v", failedComponents)
	}

	log.Printf("[LSP] LSP component installation completed successfully")
	return nil
}
