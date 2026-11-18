package catalog

import (
	"regexp"
	"strings"
	"testing"
)

func TestStableCatalogIsValid(t *testing.T) {
	catalog, err := LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Revision != "2026-11-18-v2.9" {
		t.Fatalf("Revision = %q", catalog.Revision)
	}
	if len(catalog.Components) != 4 {
		t.Fatalf("Components = %d, want 4", len(catalog.Components))
	}
}

func TestOrderedComponentsPlacesDependenciesFirst(t *testing.T) {
	catalog, err := LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	bundle, ok := catalog.Bundle("luatools")
	if !ok {
		t.Fatal("luatools bundle not found")
	}
	components, err := catalog.OrderedComponents(bundle)
	if err != nil {
		t.Fatal(err)
	}

	got := make([]string, 0, len(components))
	for _, component := range components {
		got = append(got, component.ID)
	}
	want := "slsteam-moon,lumen,luatools-moon"
	if strings.Join(got, ",") != want {
		t.Fatalf("order = %v, want %s", got, want)
	}
}

func TestCatalogRejectsInsecureArtifactURL(t *testing.T) {
	catalog, err := LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Components[0].Artifact.URL = "http://example.com/package.zip"
	if err := catalog.Validate(); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("Validate() error = %v, want HTTPS error", err)
	}
}

func TestCatalogRejectsInsecureMirror(t *testing.T) {
	catalog, err := LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Components[0].Artifact.Mirrors = append(catalog.Components[0].Artifact.Mirrors, "http://example.com/package.zip")
	if err := catalog.Validate(); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("Validate() error = %v, want HTTPS error", err)
	}
}

func TestCatalogRejectsDuplicateArtifactSource(t *testing.T) {
	catalog, err := LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	artifact := &catalog.Components[0].Artifact
	artifact.Mirrors = append(artifact.Mirrors, artifact.URL)
	if err := catalog.Validate(); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("Validate() error = %v, want duplicate source error", err)
	}
}

// GitHub release assets can be replaced in place under the same tag, which
// breaks the pinned size and SHA-256. Primary sources must be commit-pinned;
// release URLs may only appear as mirrors.
func TestStableCatalogPinsPrimarySourcesToCommits(t *testing.T) {
	catalog, err := LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	commit := regexp.MustCompile(`[/@][0-9a-f]{40}/`)
	for _, component := range catalog.Components {
		if !commit.MatchString(component.Artifact.URL) {
			t.Errorf("%s primary URL %s is not pinned to a commit", component.ID, component.Artifact.URL)
		}
	}
}

func TestCatalogRejectsDependencyCycle(t *testing.T) {
	catalog, err := LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Components[0].Dependencies = []string{"luatools-moon"}
	if err := catalog.Validate(); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("Validate() error = %v, want cycle error", err)
	}
}

func TestCatalogRejectsChangedVerifiedScriptContract(t *testing.T) {
	catalog, err := LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Components[0].Install.Arguments = []string{"install", "--system"}
	if err := catalog.Validate(); err == nil || !strings.Contains(err.Error(), "single install argument") {
		t.Fatalf("Validate() error = %v, want verified-script contract error", err)
	}
}

func TestCatalogRejectsUnsafeInstallMarker(t *testing.T) {
	catalog, err := LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Components[1].Install.Validate = append(catalog.Components[1].Install.Validate, "../escape")
	if err := catalog.Validate(); err == nil || !strings.Contains(err.Error(), "unsafe relative install path") {
		t.Fatalf("Validate() error = %v, want unsafe path error", err)
	}
}
