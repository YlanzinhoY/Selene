package planner

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/selene-linux/selene/internal/catalog"
)

func TestBuildLuaToolsPlan(t *testing.T) {
	source, err := catalog.LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	env := Environment{
		OS: "linux", Arch: "amd64", Home: "/home/player",
		XDGDataHome: "/mnt/player-data", XDGCacheHome: "/home/player/.cache",
		XDGStateHome: "/home/player/.local/state",
	}
	plan, err := Build(source, "luatools", env)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Ready || len(plan.Blockers) != 0 {
		t.Fatalf("plan should be ready on linux/amd64, blockers = %#v", plan.Blockers)
	}
	if len(plan.Operations) < 15 {
		t.Fatalf("Operations = %d, want a detailed plan", len(plan.Operations))
	}

	var pluginTarget string
	for _, operation := range plan.Operations {
		if operation.Component == "luatools-moon" && operation.Phase == "activate" {
			pluginTarget = operation.Target
		}
	}
	want := filepath.Join("/home/player", ".local", "share", "Lumen", "luatools")
	if pluginTarget != want {
		t.Fatalf("plugin target = %q, want %q", pluginTarget, want)
	}
}

func TestBuildAddsPlatformBlocker(t *testing.T) {
	source, err := catalog.LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Build(source, "luatools", Environment{
		OS: "windows", Arch: "amd64", Home: `C:\Users\player`,
		XDGDataHome: `C:\Users\player\.local\share`, XDGCacheHome: `C:\Users\player\.cache`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blockers) != 1 {
		t.Fatalf("Blockers = %#v, want only the platform blocker", plan.Blockers)
	}
}

func TestBuildRejectsUnknownBundle(t *testing.T) {
	source, err := catalog.LoadStable()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Build(source, "missing", Environment{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Build() error = %v", err)
	}
}
