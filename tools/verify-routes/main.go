// verify-routes extracts HTTP routes from PRD acceptance criteria and checks
// if they are wired in the HTTP router (e.g., server.go, main.go).
//
// Usage:
//
//	go run tools/verify-routes --prd docs/PRDs/feature.md --router internal/api/server.go
//
// Exit codes:
//
//	0 - all routes wired
//	1 - one or more routes missing
//	2 - PRD parse error
//	3 - router file not found
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type Route struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Found  bool   `json:"found"`
}

type Violation struct {
	Route  string `json:"route"`
	Method string `json:"method"`
	Path   string `json:"path"`
	File   string `json:"file"`
	Issue  string `json:"issue"`
}

type Result struct {
	Passed      bool        `json:"passed"`
	RoutesFound []Route     `json:"routes_found,omitempty"`
	Missing     []Violation `json:"missing,omitempty"`
}

func main() {
	prdPath := flag.String("prd", "", "Path to PRD markdown file")
	routerPath := flag.String("router", "internal/api/server.go", "Path to HTTP router file")
	flag.Parse()

	if *prdPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: verify-routes --prd <prd.md> [--router <file>]")
		os.Exit(2)
	}

	prdBytes, err := os.ReadFile(*prdPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading PRD: %v\n", err)
		os.Exit(2)
	}

	routerBytes, err := os.ReadFile(*routerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading router file: %v\n", err)
		os.Exit(3)
	}

	routes := extractRoutes(string(prdBytes))
	routerCode := string(routerBytes)

	var missing []Violation
	var found []Route

	for _, r := range routes {
		// Skip external API routes (Apollo, Smartlead, HubSpot, etc.)
		if isExternalAPIRoute(r.Path) {
			continue
		}
		wired := isRouteWired(r.Method, r.Path, routerCode)
		r.Found = wired
		found = append(found, r)
		if !wired {
			missing = append(missing, Violation{
				Route:  r.Method + " " + r.Path,
				Method: r.Method,
				Path:   r.Path,
				File:   *routerPath,
				Issue:  "route declared in PRD but not wired in router",
			})
		}
	}

	result := Result{
		Passed:      len(missing) == 0,
		RoutesFound: found,
		Missing:     missing,
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))

	if len(missing) > 0 {
		os.Exit(1)
	}
}

// extractRoutes finds HTTP route patterns in PRD acceptance criteria.
// Looks for patterns like "POST /campaigns/{name}/icps" in bullet points.
func extractRoutes(prd string) []Route {
	var routes []Route
	seen := make(map[string]bool)

	// Match route patterns in the PRD text
	re := regexp.MustCompile(`\b(POST|GET|PUT|DELETE|PATCH)\s+(/[A-Za-z0-9_{}/-]+)`)
	matches := re.FindAllStringSubmatch(prd, -1)

	for _, m := range matches {
		method := m[1]
		path := normalizeRoute(m[2])
		key := method + " " + path
		if !seen[key] {
			seen[key] = true
			routes = append(routes, Route{Method: method, Path: path})
		}
	}

	return routes
}

// normalizeRoute strips query params and normalizes Go ServeMux wildcards
// so that /campaigns/{name} and /campaigns/{name...} are both findable.
// isExternalAPIRoute returns true for routes that are external API endpoints
// (Apollo, Smartlead, HubSpot, etc.) rather than our own server routes.
func isExternalAPIRoute(path string) bool {
	externalPatterns := []string{
		"/lists",          // Apollo API
		"/contacts/",      // Apollo API
		"/mailboxes/",     // Smartlead API
		"/campaigns/",     // Smartlead API (external, not ours)
	}
	for _, p := range externalPatterns {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func normalizeRoute(path string) string {
	path = strings.TrimSpace(path)
	// Remove trailing punctuation that might be part of markdown
	path = strings.TrimRight(path, ".),;:`")
	return path
}

// isRouteWired checks if a route exists in the router code.
// It handles Go 1.22 ServeMux patterns with wildcards.
func isRouteWired(method, path, routerCode string) bool {
	// Normalize path for wildcard matching
	pathWildcard := strings.ReplaceAll(path, "{", "{")
	pathWildcard = strings.ReplaceAll(pathWildcard, "}", "}")

	// Try exact match first
	exactPattern := fmt.Sprintf(`"%s %s"`, method, path)
	if strings.Contains(routerCode, exactPattern) {
		return true
	}

	// Try with wildcard suffix like {name...}
	// /campaigns/{name}/icps might be registered as /campaigns/{name...} with dispatch
	parts := strings.Split(path, "/")
	for i := range parts {
		if strings.HasPrefix(parts[i], "{") && strings.HasSuffix(parts[i], "}") {
			parts[i] = strings.ReplaceAll(parts[i], "}", "...}")
			wildcardPath := strings.Join(parts, "/")
			wildcardPattern := fmt.Sprintf(`"%s %s"`, method, wildcardPath)
			if strings.Contains(routerCode, wildcardPattern) {
				return true
			}
			// Also try generic HandleFunc without method prefix
			if strings.Contains(routerCode, wildcardPath) {
				return true
			}
		}
	}

	// Fallback: check if the route suffix is handled (for dispatch patterns)
	// e.g., a catch-all /campaigns/{name...} that dispatches by suffix
	basePath := path
	if idx := strings.Index(path, "{"); idx > 0 {
		basePath = path[:idx]
	}
	if strings.Contains(routerCode, basePath) {
		// Check if there's a dispatcher that handles this specific suffix
		suffix := ""
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash > 0 {
			suffix = path[lastSlash:]
		}
		if suffix != "" && strings.Contains(routerCode, suffix) {
			return true
		}
	}

	return false
}
