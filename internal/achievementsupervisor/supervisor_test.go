package achievementsupervisor

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSupervisorOrdersBackendBeforeSteamAndStopsItGracefully(t *testing.T) {
	backend := newFakeProcess(true)
	steam := newFakeProcess(true)
	var steamRunning atomic.Bool
	starter := &fakeStarter{
		results: []fakeStartResult{{process: backend}, {process: steam}},
		onStart: func(spec ProcessSpec, index int) {
			if spec.Name == "Steam" {
				steamRunning.Store(true)
				steam.exit(nil)
				go func() {
					time.Sleep(15 * time.Millisecond)
					steamRunning.Store(false)
				}()
			}
		},
	}

	supervisor := newSupervisor(testConfig(), starter, healthy, steamRunning.Load)
	result, err := supervisor.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.SteamStarted || !result.BackendBecameReady || result.Degraded {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := starter.names(); len(got) != 2 || got[0] != "backend" || got[1] != "Steam" {
		t.Fatalf("start order = %v", got)
	}
	if backend.signalCount() != 1 || backend.killed.Load() {
		t.Fatalf("backend shutdown = signals %d, killed %v", backend.signalCount(), backend.killed.Load())
	}
}

func TestSupervisorStartsSteamWhenBackendCannotStart(t *testing.T) {
	steam := newFakeProcess(true)
	var steamRunning atomic.Bool
	starter := &fakeStarter{
		results: []fakeStartResult{{err: errors.New("backend unavailable")}, {process: steam}},
		onStart: func(spec ProcessSpec, index int) {
			if spec.Name == "Steam" {
				steamRunning.Store(true)
				steam.exit(nil)
				go func() {
					time.Sleep(15 * time.Millisecond)
					steamRunning.Store(false)
				}()
			}
		},
	}
	config := testConfig()
	config.RestartBackoff = time.Second

	result, err := newSupervisor(config, starter, healthy, steamRunning.Load).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.SteamStarted || !result.Degraded || result.LastBackendError == "" {
		t.Fatalf("degraded result = %+v", result)
	}
	if got := starter.names(); len(got) < 2 || got[0] != "backend" || got[1] != "Steam" {
		t.Fatalf("Steam was not started after backend failure: %v", got)
	}
}

func TestSupervisorRestartsBackendWhileSteamRuns(t *testing.T) {
	backend1 := newFakeProcess(true)
	steam := newFakeProcess(true)
	backend2 := newFakeProcess(true)
	var steamRunning atomic.Bool
	starter := &fakeStarter{
		results: []fakeStartResult{{process: backend1}, {process: steam}, {process: backend2}},
		onStart: func(spec ProcessSpec, index int) {
			switch index {
			case 1:
				steamRunning.Store(true)
				steam.exit(nil)
				go func() {
					time.Sleep(5 * time.Millisecond)
					backend1.exit(errors.New("crashed"))
				}()
			case 2:
				go func() {
					time.Sleep(12 * time.Millisecond)
					steamRunning.Store(false)
				}()
			}
		},
	}

	result, err := newSupervisor(testConfig(), starter, healthy, steamRunning.Load).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.BackendRestarts != 1 || result.RestartAttempts != 1 || !result.Degraded {
		t.Fatalf("restart result = %+v", result)
	}
	if backend2.signalCount() != 1 {
		t.Fatalf("restarted backend SIGTERM count = %d", backend2.signalCount())
	}
}

func TestSupervisorForcesBackendOnlyAfterShutdownTimeout(t *testing.T) {
	backend := newFakeProcess(false)
	steam := newFakeProcess(true)
	var steamRunning atomic.Bool
	starter := &fakeStarter{
		results: []fakeStartResult{{process: backend}, {process: steam}},
		onStart: func(spec ProcessSpec, index int) {
			if spec.Name == "Steam" {
				steamRunning.Store(true)
				steam.exit(nil)
				go func() {
					time.Sleep(8 * time.Millisecond)
					steamRunning.Store(false)
				}()
			}
		},
	}
	config := testConfig()
	config.ShutdownTimeout = 5 * time.Millisecond

	result, err := newSupervisor(config, starter, healthy, steamRunning.Load).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.ForcedBackendStop || !backend.killed.Load() || backend.signalCount() != 1 {
		t.Fatalf("forced shutdown result = %+v, signals = %d, killed = %v", result, backend.signalCount(), backend.killed.Load())
	}
}

func TestSupervisorRejectsAlreadyRunningSteam(t *testing.T) {
	starter := &fakeStarter{}
	_, err := newSupervisor(testConfig(), starter, healthy, func() bool { return true }).Run(context.Background())
	if err == nil || len(starter.names()) != 0 {
		t.Fatalf("already-running Steam error = %v, starts = %v", err, starter.names())
	}
}

func TestSupervisorDoesNotStartProcessesAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	starter := &fakeStarter{}
	_, err := newSupervisor(testConfig(), starter, healthy, func() bool { return false }).Run(ctx)
	if !errors.Is(err, context.Canceled) || len(starter.names()) != 0 {
		t.Fatalf("cancelled run error = %v, starts = %v", err, starter.names())
	}
}

func TestSupervisorValidatesLoopbackHealthURL(t *testing.T) {
	config := testConfig()
	config.HealthURL = "http://0.0.0.0:48212/v1/health"
	_, err := newSupervisor(config, &fakeStarter{}, healthy, func() bool { return false }).Run(context.Background())
	if err == nil {
		t.Fatal("external health URL was accepted")
	}
}

func testConfig() Config {
	return Config{
		Backend:           ProcessSpec{Name: "backend", Path: "/backend", Output: io.Discard},
		Steam:             ProcessSpec{Name: "Steam", Path: "/steam", Output: io.Discard},
		HealthURL:         "http://127.0.0.1:48212/v1/health",
		ReadyTimeout:      20 * time.Millisecond,
		SteamStartTimeout: 30 * time.Millisecond,
		SteamExitGrace:    2 * time.Millisecond,
		PollInterval:      time.Millisecond,
		RestartBackoff:    time.Millisecond,
		ShutdownTimeout:   20 * time.Millisecond,
	}
}

func healthy(context.Context, string) error {
	return nil
}

type fakeStartResult struct {
	process Process
	err     error
}

type fakeStarter struct {
	mu      sync.Mutex
	results []fakeStartResult
	started []ProcessSpec
	onStart func(ProcessSpec, int)
}

func (s *fakeStarter) Start(spec ProcessSpec) (Process, error) {
	s.mu.Lock()
	index := len(s.started)
	s.started = append(s.started, spec)
	if len(s.results) == 0 {
		s.mu.Unlock()
		return nil, errors.New("unexpected process start")
	}
	result := s.results[0]
	s.results = s.results[1:]
	hook := s.onStart
	s.mu.Unlock()
	if hook != nil {
		hook(spec, index)
	}
	return result.process, result.err
}

func (s *fakeStarter) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.started))
	for _, spec := range s.started {
		names = append(names, spec.Name)
	}
	return names
}

type fakeProcess struct {
	done         chan struct{}
	exitOnce     sync.Once
	errMu        sync.Mutex
	err          error
	mu           sync.Mutex
	signals      []os.Signal
	exitOnSignal bool
	killed       atomic.Bool
}

func newFakeProcess(exitOnSignal bool) *fakeProcess {
	return &fakeProcess{done: make(chan struct{}), exitOnSignal: exitOnSignal}
}

func (p *fakeProcess) Wait() error {
	<-p.done
	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.err
}

func (p *fakeProcess) Signal(signal os.Signal) error {
	p.mu.Lock()
	p.signals = append(p.signals, signal)
	p.mu.Unlock()
	if p.exitOnSignal {
		p.exit(nil)
	}
	return nil
}

func (p *fakeProcess) Kill() error {
	p.killed.Store(true)
	p.exit(errors.New("killed"))
	return nil
}

func (p *fakeProcess) exit(err error) {
	p.exitOnce.Do(func() {
		p.errMu.Lock()
		p.err = err
		p.errMu.Unlock()
		close(p.done)
	})
}

func (p *fakeProcess) signalCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.signals)
}
