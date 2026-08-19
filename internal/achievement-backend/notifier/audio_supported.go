//go:build !linux || cgo

package notifier

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/gopxl/beep/v2/wav"
	backend "github.com/selene-linux/selene/internal/achievement-backend"
)

var (
	speakerMu          sync.Mutex
	speakerInitialized bool
)

func initSpeaker() bool {
	speakerMu.Lock()
	defer speakerMu.Unlock()

	if speakerInitialized {
		return true
	}

	sampleRate := beep.SampleRate(44100)
	if err := speaker.Init(sampleRate, sampleRate.N(time.Second/10)); err != nil {
		slog.Warn("Failed to initialize audio speaker", "error", err)
		return false
	}

	speakerInitialized = true
	return true
}

// PlaySound plays a sound file asynchronously when the platform audio backend
// is available. Linux static builds use the no-op implementation instead.
func (s *Service) PlaySound(filename string) error {
	if filename == "" {
		return nil
	}

	soundPath := filepath.Join(backend.MediaDir, filename)
	if _, err := os.Stat(soundPath); err != nil {
		return nil
	}

	go func() {
		if !initSpeaker() {
			return
		}

		file, err := os.Open(soundPath)
		if err != nil {
			slog.Warn("Failed to open sound file", "filename", filename, "error", err)
			return
		}
		defer file.Close()

		streamer, _, err := wav.Decode(file)
		if err != nil {
			slog.Warn("Failed to decode sound file", "filename", filename, "error", err)
			return
		}
		defer streamer.Close()

		done := make(chan struct{})
		speaker.Play(beep.Seq(streamer, beep.Callback(func() {
			close(done)
		})))
		<-done
	}()

	return nil
}
