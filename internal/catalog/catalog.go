package catalog

import (
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
)

//go:embed manifests/*.json
var manifestFS embed.FS

const stableManifest = "manifests/stable.json"

// Catalog is the versioned source of installable Selene components.
type Catalog struct {
	SchemaVersion int         `json:"schema_version"`
	Revision      string      `json:"revision"`
	Channel       string      `json:"channel"`
	Bundles       []Bundle    `json:"bundles"`
	Components    []Component `json:"components"`
}

// Bundle is an ordered, user-facing group of components.
type Bundle struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Components  []string `json:"components"`
}

// Component describes one pinned upstream artifact and how it will be staged.
type Component struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Version      string      `json:"version"`
	Description  string      `json:"description"`
	Optional     bool        `json:"optional,omitempty"`
	Dependencies []string    `json:"dependencies"`
	Source       Source      `json:"source"`
	Artifact     Artifact    `json:"artifact"`
	Install      InstallSpec `json:"install"`
}

type Source struct {
	Provider   string `json:"provider"`
	Repository string `json:"repository"`
	Reference  string `json:"reference"`
}

type Artifact struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Format string `json:"format"`
}

type InstallSpec struct {
	Strategy        string   `json:"strategy"`
	Destination     string   `json:"destination"`
	StripComponents int      `json:"strip_components"`
	Entrypoint      string   `json:"entrypoint,omitempty"`
	Arguments       []string `json:"arguments,omitempty"`
	Preserve        []string `json:"preserve,omitempty"`
	Executables     []string `json:"executables,omitempty"`
	Validate        []string `json:"validate"`
}

// LoadStable loads and validates the catalog bundled into the Selene binary.
func LoadStable() (Catalog, error) {
	data, err := manifestFS.ReadFile(stableManifest)
	if err != nil {
		return Catalog{}, fmt.Errorf("read stable catalog: %w", err)
	}
	return Parse(data)
}

// Parse decodes and validates catalog JSON.
func Parse(data []byte) (Catalog, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()

	var catalog Catalog
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode catalog: %w", err)
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

// Validate rejects incomplete, ambiguous or unverifiable catalog entries.
func (c Catalog) Validate() error {
	if c.SchemaVersion != 1 {
		return fmt.Errorf("unsupported catalog schema %d", c.SchemaVersion)
	}
	if c.Revision == "" || c.Channel == "" {
		return errors.New("catalog revision and channel are required")
	}
	if len(c.Components) == 0 {
		return errors.New("catalog has no components")
	}

	components := make(map[string]Component, len(c.Components))
	for _, component := range c.Components {
		if component.ID == "" || component.Name == "" || component.Version == "" {
			return errors.New("component id, name and version are required")
		}
		if _, exists := components[component.ID]; exists {
			return fmt.Errorf("duplicate component %q", component.ID)
		}
		if err := validateComponent(component); err != nil {
			return fmt.Errorf("component %s: %w", component.ID, err)
		}
		components[component.ID] = component
	}

	for _, component := range c.Components {
		for _, dependency := range component.Dependencies {
			if _, ok := components[dependency]; !ok {
				return fmt.Errorf("component %s references unknown dependency %s", component.ID, dependency)
			}
			if dependency == component.ID {
				return fmt.Errorf("component %s depends on itself", component.ID)
			}
		}
	}

	if err := validateDependencyGraph(components); err != nil {
		return err
	}

	bundles := make(map[string]bool, len(c.Bundles))
	for _, bundle := range c.Bundles {
		if bundle.ID == "" || bundle.Name == "" || len(bundle.Components) == 0 {
			return errors.New("bundle id, name and components are required")
		}
		if bundles[bundle.ID] {
			return fmt.Errorf("duplicate bundle %q", bundle.ID)
		}
		bundles[bundle.ID] = true
		for _, component := range bundle.Components {
			if _, ok := components[component]; !ok {
				return fmt.Errorf("bundle %s references unknown component %s", bundle.ID, component)
			}
		}
	}

	return nil
}

// Bundle returns one bundle by ID.
func (c Catalog) Bundle(id string) (Bundle, bool) {
	for _, bundle := range c.Bundles {
		if bundle.ID == id {
			return bundle, true
		}
	}
	return Bundle{}, false
}

// Component returns one component by ID.
func (c Catalog) Component(id string) (Component, bool) {
	for _, component := range c.Components {
		if component.ID == id {
			return component, true
		}
	}
	return Component{}, false
}

// OrderedComponents expands a bundle with dependencies before dependents.
func (c Catalog) OrderedComponents(bundle Bundle) ([]Component, error) {
	var ordered []Component
	visited := make(map[string]bool)
	var visit func(string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		component, ok := c.Component(id)
		if !ok {
			return fmt.Errorf("unknown component %s", id)
		}
		for _, dependency := range component.Dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		visited[id] = true
		ordered = append(ordered, component)
		return nil
	}

	for _, id := range bundle.Components {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func validateComponent(component Component) error {
	if component.Source.Provider == "" || component.Source.Repository == "" || component.Source.Reference == "" {
		return errors.New("source provider, repository and reference are required")
	}
	if component.Artifact.Name == "" || component.Artifact.Size <= 0 {
		return errors.New("artifact name and positive size are required")
	}
	parsedURL, err := url.Parse(component.Artifact.URL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return errors.New("artifact URL must be absolute HTTPS")
	}
	checksum, err := hex.DecodeString(component.Artifact.SHA256)
	if err != nil || len(checksum) != 32 || component.Artifact.SHA256 != strings.ToLower(component.Artifact.SHA256) {
		return errors.New("artifact sha256 must contain 64 lowercase hexadecimal characters")
	}
	if !slices.Contains([]string{"zip", "file"}, component.Artifact.Format) {
		return fmt.Errorf("unsupported artifact format %q", component.Artifact.Format)
	}
	if component.Install.Strategy == "" || component.Install.Destination == "" {
		return errors.New("install strategy and destination are required")
	}
	if !slices.Contains([]string{"extract", "replace-preserve", "copy", "verified-script"}, component.Install.Strategy) {
		return fmt.Errorf("unsupported install strategy %q", component.Install.Strategy)
	}
	if !strings.HasPrefix(component.Install.Destination, "${XDG_DATA_HOME}/") &&
		!strings.HasPrefix(component.Install.Destination, "${HOME}/") {
		return errors.New("install destination must be inside HOME or XDG_DATA_HOME")
	}
	if len(component.Install.Validate) == 0 {
		return errors.New("at least one validation marker is required")
	}
	for _, marker := range append(append(append([]string(nil), component.Install.Validate...), component.Install.Executables...), component.Install.Preserve...) {
		if marker == "" || strings.HasPrefix(marker, "/") || strings.Contains(marker, `\`) {
			return fmt.Errorf("unsafe relative install path %q", marker)
		}
		cleaned := path.Clean(marker)
		if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != marker {
			return fmt.Errorf("unsafe relative install path %q", marker)
		}
	}
	if component.Install.Strategy == "verified-script" {
		entrypoint := component.Install.Entrypoint
		if entrypoint == "" || strings.HasPrefix(entrypoint, "/") || strings.Contains(entrypoint, `\`) {
			return errors.New("verified script entrypoint must be a safe relative path")
		}
		cleaned := path.Clean(entrypoint)
		if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return errors.New("verified script entrypoint escapes the staged artifact")
		}
		if !slices.Contains(component.Install.Validate, cleaned) {
			return errors.New("verified script entrypoint must also be a validation marker")
		}
		if len(component.Install.Arguments) != 1 || component.Install.Arguments[0] != "install" {
			return errors.New("verified script must declare the single install argument")
		}
	}
	return nil
}

func validateDependencyGraph(components map[string]Component) error {
	const (
		visiting = iota + 1
		visited
	)
	states := make(map[string]int, len(components))
	var visit func(string) error
	visit = func(id string) error {
		switch states[id] {
		case visiting:
			return fmt.Errorf("dependency cycle includes %s", id)
		case visited:
			return nil
		}
		states[id] = visiting
		for _, dependency := range components[id].Dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		states[id] = visited
		return nil
	}

	for id := range components {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
