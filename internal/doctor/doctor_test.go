package doctor

import (
	"reflect"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	data := `
NAME="CachyOS"
ID=cachyos
PRETTY_NAME='CachyOS Linux'
# ignored
`

	got := parseOSRelease(data)
	if got["ID"] != "cachyos" {
		t.Fatalf("ID = %q, want cachyos", got["ID"])
	}
	if got["PRETTY_NAME"] != "CachyOS Linux" {
		t.Fatalf("PRETTY_NAME = %q, want CachyOS Linux", got["PRETTY_NAME"])
	}
}

func TestParseLibraryFolders(t *testing.T) {
	data := `
"libraryfolders"
{
    "0" { "path" "/home/player/.local/share/Steam" }
    "1" { "path" "/mnt/games/SteamLibrary" }
    "2" { "path" "/mnt/games/SteamLibrary" }
}
`

	want := []string{"/home/player/.local/share/Steam", "/mnt/games/SteamLibrary"}
	if got := parseLibraryFolders(data); !reflect.DeepEqual(got, want) {
		t.Fatalf("parseLibraryFolders() = %#v, want %#v", got, want)
	}
}

func TestFinishCountsStatuses(t *testing.T) {
	report := finish(Report{Checks: []Check{
		{Status: StatusOK},
		{Status: StatusOK},
		{Status: StatusWarning},
		{Status: StatusError},
		{Status: StatusInfo},
	}})

	want := Summary{OK: 2, Warnings: 1, Errors: 1, Info: 1}
	if report.Summary != want {
		t.Fatalf("Summary = %#v, want %#v", report.Summary, want)
	}
}
