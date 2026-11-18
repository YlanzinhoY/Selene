package artifact

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/selene-linux/selene/internal/catalog"
)

func TestFetcherDownloadsVerifiesAndReusesCache(t *testing.T) {
	payload := []byte("verified selene artifact")
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	component := catalog.Component{
		ID: "sample", Name: "Sample", Version: "v1",
		Artifact: catalog.Artifact{
			Name: "sample.bin", URL: server.URL + "/sample.bin", Size: int64(len(payload)), SHA256: hash, Format: "file",
		},
		Install: catalog.InstallSpec{Validate: []string{"sample.bin"}},
	}
	fetcher := &Fetcher{Client: server.Client()}
	cache := filepath.Join(t.TempDir(), "cache")

	first, err := fetcher.Fetch(context.Background(), component, cache)
	if err != nil {
		t.Fatal(err)
	}
	if first.Cached {
		t.Fatal("first result unexpectedly came from cache")
	}
	second, err := fetcher.Fetch(context.Background(), component, cache)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Cached {
		t.Fatal("second result should come from cache")
	}
	if requests.Load() != 1 {
		t.Fatalf("HTTP requests = %d, want 1", requests.Load())
	}
}

func TestFetcherRejectsWrongHash(t *testing.T) {
	payload := []byte("tampered")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	component := catalog.Component{
		ID: "sample", Name: "Sample", Version: "v1",
		Artifact: catalog.Artifact{
			Name: "sample.bin", URL: server.URL, Size: int64(len(payload)), SHA256: strings.Repeat("0", 64), Format: "file",
		},
		Install: catalog.InstallSpec{Validate: []string{"sample.bin"}},
	}
	_, err := (&Fetcher{Client: server.Client()}).Fetch(context.Background(), component, filepath.Join(t.TempDir(), "cache"))
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("Fetch() error = %v, want checksum mismatch", err)
	}
}

func mirroredComponent(primary string, payload []byte, mirrors ...string) catalog.Component {
	return catalog.Component{
		ID: "sample", Name: "Sample", Version: "v1",
		Artifact: catalog.Artifact{
			Name: "sample.bin", URL: primary, Mirrors: mirrors, Size: int64(len(payload)),
			SHA256: fmt.Sprintf("%x", sha256.Sum256(payload)), Format: "file",
		},
		Install: catalog.InstallSpec{Validate: []string{"sample.bin"}},
	}
}

func TestFetcherFallsBackToMirrorWhenPrimaryBytesChanged(t *testing.T) {
	payload := []byte("verified selene artifact")
	var primaryHits, mirrorHits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := payload
		switch r.URL.Path {
		case "/primary":
			primaryHits.Add(1)
			body = append([]byte("asset replaced upstream: "), payload...)
		case "/mirror":
			mirrorHits.Add(1)
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		_, _ = w.Write(body)
	}))
	defer server.Close()

	component := mirroredComponent(server.URL+"/primary", payload, server.URL+"/mirror")
	result, err := (&Fetcher{Client: server.Client()}).Fetch(context.Background(), component, filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Cached || primaryHits.Load() != 1 || mirrorHits.Load() != 1 {
		t.Fatalf("cached=%v primary=%d mirror=%d, want one fresh request to each", result.Cached, primaryHits.Load(), mirrorHits.Load())
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload) {
		t.Fatalf("cached artifact = %q, want the mirror payload", data)
	}
}

func TestFetcherSkipsMirrorsWhenPrimaryVerifies(t *testing.T) {
	payload := []byte("verified selene artifact")
	var mirrorHits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mirror" {
			mirrorHits.Add(1)
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	component := mirroredComponent(server.URL+"/primary", payload, server.URL+"/mirror")
	if _, err := (&Fetcher{Client: server.Client()}).Fetch(context.Background(), component, filepath.Join(t.TempDir(), "cache")); err != nil {
		t.Fatal(err)
	}
	if mirrorHits.Load() != 0 {
		t.Fatalf("mirror requests = %d, want 0", mirrorHits.Load())
	}
}

func TestFetcherReportsEverySourceWhenAllFail(t *testing.T) {
	payload := []byte("verified selene artifact")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tampered" {
			http.NotFound(w, r)
			return
		}
		tampered := strings.Repeat("x", len(payload))
		w.Header().Set("Content-Length", fmt.Sprint(len(tampered)))
		_, _ = w.Write([]byte(tampered))
	}))
	defer server.Close()

	cache := filepath.Join(t.TempDir(), "cache")
	component := mirroredComponent(server.URL+"/missing", payload, server.URL+"/tampered")
	_, err := (&Fetcher{Client: server.Client()}).Fetch(context.Background(), component, cache)
	if err == nil {
		t.Fatal("Fetch() succeeded, want every source to fail")
	}
	for _, want := range []string{"every source failed", "HTTP 404", "SHA-256 mismatch"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Fetch() error = %q, want it to mention %q", err, want)
		}
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache keeps %d leftover files after failed downloads", len(entries))
	}
}

func TestInspectZIPWithStrippedRoot(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "package.zip")
	writeZIP(t, filePath, map[string]string{
		"release/":               "",
		"release/bin/tool":       "binary",
		"release/res/config.yml": "config",
	})
	component := catalog.Component{
		Artifact: catalog.Artifact{Format: "zip"},
		Install: catalog.InstallSpec{
			StripComponents: 1,
			Executables:     []string{"bin/tool"},
			Validate:        []string{"res/config.yml"},
		},
	}
	if err := Inspect(filePath, component); err != nil {
		t.Fatal(err)
	}
}

func TestInspectZIPRejectsTraversal(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "package.zip")
	writeZIP(t, filePath, map[string]string{"../escape": "bad"})
	component := catalog.Component{
		Artifact: catalog.Artifact{Format: "zip"},
		Install:  catalog.InstallSpec{Validate: []string{"escape"}},
	}
	if err := Inspect(filePath, component); err == nil || !strings.Contains(err.Error(), "escapes destination") {
		t.Fatalf("Inspect() error = %v, want traversal rejection", err)
	}
}

func TestInspectZIPRejectsMissingMarker(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "package.zip")
	writeZIP(t, filePath, map[string]string{"other.txt": "content"})
	component := catalog.Component{
		Artifact: catalog.Artifact{Format: "zip"},
		Install:  catalog.InstallSpec{Validate: []string{"plugin.json"}},
	}
	if err := Inspect(filePath, component); err == nil || !strings.Contains(err.Error(), "missing required") {
		t.Fatalf("Inspect() error = %v, want missing marker rejection", err)
	}
}

func writeZIP(t *testing.T, filePath string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
