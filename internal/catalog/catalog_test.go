package catalog

import (
	"strings"
	"testing"
)

func TestStableCatalogIsValid(t *testing.T) {
	catalog, err := LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Revision != "2026-08-15-v2.8" {
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
