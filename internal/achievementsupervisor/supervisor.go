package achievementsupervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/selene-linux/selene/internal/achievementserver"
	"github.com/selene-linux/selene/internal/planner"
	"github.com/selene-linux/selene/internal/plugins"
)

const trustedPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// ProcessSpec describes one process owned by the achievement session.
type ProcessSpec struct {
	Name                string
	Path                string
	Args                []string
	Env                 []string
	Output              io.Writer
	TerminateWithParent bool
}

// Process is the small process surface needed by the supervisor. Keeping it
// narrow makes lifecycle ordering and failure recovery testable without Steam.
type Process interface {
	Wait() error
	Signal(os.Signal) error
	Kill() error
}

// ProcessStarter creates a process without attaching it to a shell.
type ProcessStarter interface {
	Start(ProcessSpec) (Process, error)
}

// HealthChecker reports whether the backend API can serve consumers.
type HealthChecker func(context.Context, string) error

// Config contains the complete, explicit achievement-session policy.
type Config struct {
	Backend           ProcessSpec
	Steam             ProcessSpec
	HealthURL         string
	ReadyTimeout      time.Duration
	SteamStartTimeout time.Duration
	SteamExitGrace    time.Duration
	PollInterval      time.Duration
	RestartBackoff    time.Duration
	ShutdownTimeout   time.Duration
}

// Result summarizes a completed Steam session. Backend failures are reported
// here as degraded operation and do not become Steam-startup failures.
type Result struct {
	SteamStarted       bool
	BackendBecameReady bool
	Degraded           bool
	BackendRestarts    int
	RestartAttempts    int
	ForcedBackendStop  bool
	LastBackendError   string
}

// Supervisor owns the backend only for the lifetime of one Steam session.
type Supervisor struct {
	config       Config
	starter      ProcessStarter
	health       HealthChecker
	steamRunning func() bool
	now          func() time.Time
}

// NewDefault builds the production supervisor for the detected Selene
// environment. It does not start any process.
func NewDefault(env planner.Environment, output io.Writer) (*Supervisor, error) {
	if env.OS != "linux" || env.Arch != "amd64" {
		return nil, fmt.Errorf("achievement sessions require Linux amd64, found %s/%s", env.OS, env.Arch)
	}
	if output == nil {
		output = io.Discard
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate Selene executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, fmt.Errorf("resolve Selene executable: %w", err)
	}
	steamWrapper := filepath.Join(env.Home, ".local", "share", "SLSsteam", "path", "steam")
	if err := requireExecutable(steamWrapper, "SLSsteam Steam wrapper"); err != nil {
		return nil, err
	}
	if err := requireExecutable(executable, "Selene executable"); err != nil {
		return nil, err
	}

	address := achievementserver.DefaultHTTPAddress
	backendEnv := append(controlledEnvironment(env), achievementserver.InternalEnvironment(address)...)
	config := Config{
		Backend: ProcessSpec{
			Name:                "achievement backend",
			Path:                executable,
			Env:                 backendEnv,
			Output:              output,
			TerminateWithParent: true,
		},
		Steam: ProcessSpec{
			Name:   "Steam through SLSsteam",
			Path:   steamWrapper,
			Args:   []string{"-silent"},
			Env:    controlledEnvironment(env),
			Output: output,
		},
		HealthURL:         "http://" + address + "/v1/health",
		ReadyTimeout:      8 * time.Second,
		SteamStartTimeout: 30 * time.Second,
		SteamExitGrace:    2 * time.Second,
		PollInterval:      250 * time.Millisecond,
		RestartBackoff:    2 * time.Second,
		ShutdownTimeout:   6 * time.Second,
	}
	return newSupervisor(config, execStarter{}, defaultHealthChecker(), plugins.SteamRunning), nil
}

func newSupervisor(config Config, starter ProcessStarter, health HealthChecker, steamRunning func() bool) *Supervisor {
	if config.Backend.Output == nil {
		config.Backend.Output = io.Discard
	}
	if config.Steam.Output == nil {
		config.Steam.Output = io.Discard
	}
	return &Supervisor{
		config:       config,
		starter:      starter,
		health:       health,
		steamRunning: steamRunning,
		now:          time.Now,
	}
}

// Run starts and supervises one Steam session. A missing or unhealthy backend
// is degraded operation: Steam is still started and backend recovery continues
// until Steam closes.
func (s *Supervisor) Run(ctx context.Context) (Result, error) {
	var result Result
	if err := s.validate(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.steamRunning() {
		return result, errors.New("Steam is already running; close it before starting a supervised achievement session")
	}

	backend := s.startBackend(&result, false)
	if backend != nil {
		if err := s.waitForBackend(ctx, backend); err != nil {
			s.recordBackendFailure(&result, err)
			forced, stopErr := s.stopBackend(backend)
			result.ForcedBackendStop = result.ForcedBackendStop || forced
			if stopErr != nil {
				s.recordBackendFailure(&result, stopErr)
			}
			backend = nil
		} else {
			result.BackendBecameReady = true
			fmt.Fprintln(s.config.Backend.Output, "Selene: achievement backend is ready.")
		}
	}
	if err := ctx.Err(); err != nil {
		forced, stopErr := s.stopBackend(backend)
		result.ForcedBackendStop = result.ForcedBackendStop || forced
		return result, errors.Join(err, stopErr)
	}

	steam, err := s.starter.Start(s.config.Steam)
	if err != nil {
		forced, stopErr := s.stopBackend(backend)
		result.ForcedBackendStop = result.ForcedBackendStop || forced
		return result, errors.Join(fmt.Errorf("start %s: %w", s.config.Steam.Name, err), stopErr)
	}
	steamExit := trackProcess(steam)
	if err := s.waitForSteam(ctx, steamExit); err != nil {
		forced, stopErr := s.stopBackend(backend)
		result.ForcedBackendStop = result.ForcedBackendStop || forced
		return result, errors.Join(err, stopErr)
	}
	result.SteamStarted = true
	fmt.Fprintln(s.config.Steam.Output, "Selene: Steam started through the SLSsteam wrapper.")

	nextRestart := s.now()
	var steamAbsentSince time.Time
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			forced, stopErr := s.stopBackend(backend)
			result.ForcedBackendStop = result.ForcedBackendStop || forced
			return result, errors.Join(ctx.Err(), stopErr)
		case <-ticker.C:
			if s.steamRunning() {
				steamAbsentSince = time.Time{}
			} else if steamAbsentSince.IsZero() {
				steamAbsentSince = s.now()
				continue
			} else if s.now().Sub(steamAbsentSince) >= s.config.SteamExitGrace {
				forced, stopErr := s.stopBackend(backend)
				result.ForcedBackendStop = result.ForcedBackendStop || forced
				return result, stopErr
			} else {
				continue
			}

			if backend != nil && backend.exited() {
				s.recordBackendFailure(&result, backend.waitError("achievement backend exited"))
				backend = nil
				nextRestart = s.now().Add(s.config.RestartBackoff)
			}
			if backend != nil || s.now().Before(nextRestart) {
				continue
			}

			result.RestartAttempts++
			backend = s.startBackend(&result, true)
			if backend == nil {
				nextRestart = s.now().Add(s.config.RestartBackoff)
				continue
			}
			if err := s.waitForBackend(ctx, backend); err != nil {
				s.recordBackendFailure(&result, err)
				forced, stopErr := s.stopBackend(backend)
				result.ForcedBackendStop = result.ForcedBackendStop || forced
				if stopErr != nil {
					s.recordBackendFailure(&result, stopErr)
				}
				backend = nil
				nextRestart = s.now().Add(s.config.RestartBackoff)
				continue
			}
			result.BackendRestarts++
			result.BackendBecameReady = true
			fmt.Fprintln(s.config.Backend.Output, "Selene: achievement backend recovered.")
		}
	}
}

type trackedProcess struct {
	process Process
	done    chan struct{}
	errMu   sync.Mutex
	err     error
}

func trackProcess(process Process) *trackedProcess {
	tracked := &trackedProcess{process: process, done: make(chan struct{})}
	go func() {
		err := process.Wait()
		tracked.errMu.Lock()
		tracked.err = err
		tracked.errMu.Unlock()
		close(tracked.done)
	}()
	return tracked
}

func (p *trackedProcess) exited() bool {
	if p == nil {
		return true
	}
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *trackedProcess) waitError(prefix string) error {
	if p == nil {
		return nil
	}
	<-p.done
	p.errMu.Lock()
	defer p.errMu.Unlock()
	if p.err == nil {
		return errors.New(prefix)
	}
	return fmt.Errorf("%s: %w", prefix, p.err)
}

func (s *Supervisor) startBackend(result *Result, restart bool) *trackedProcess {
	process, err := s.starter.Start(s.config.Backend)
	if err != nil {
		label := "start achievement backend"
		if restart {
			label = "restart achievement backend"
		}
		s.recordBackendFailure(result, fmt.Errorf("%s: %w", label, err))
		return nil
	}
	return trackProcess(process)
}

func (s *Supervisor) waitForBackend(ctx context.Context, backend *trackedProcess) error {
	readyCtx, cancel := context.WithTimeout(ctx, s.config.ReadyTimeout)
	defer cancel()
	var lastErr error
	for {
		select {
		case <-backend.done:
			return backend.waitError("achievement backend exited before becoming ready")
		default:
		}
		if err := s.health(readyCtx, s.config.HealthURL); err == nil {
			return nil
		} else {
			lastErr = err
		}

		timer := time.NewTimer(s.config.PollInterval)
		select {
		case <-backend.done:
			timer.Stop()
			return backend.waitError("achievement backend exited before becoming ready")
		case <-readyCtx.Done():
			timer.Stop()
			if errors.Is(readyCtx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("achievement backend health check timed out: %w", lastErr)
			}
			return readyCtx.Err()
		case <-timer.C:
		}
	}
}

func (s *Supervisor) waitForSteam(ctx context.Context, steam *trackedProcess) error {
	startCtx, cancel := context.WithTimeout(ctx, s.config.SteamStartTimeout)
	defer cancel()
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()
	for {
		if s.steamRunning() {
			return nil
		}
		select {
		case <-startCtx.Done():
			if errors.Is(startCtx.Err(), context.DeadlineExceeded) {
				if steam.exited() {
					return steam.waitError("SLSsteam wrapper exited before Steam appeared")
				}
				return errors.New("Steam did not appear before the startup timeout")
			}
			return startCtx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Supervisor) stopBackend(backend *trackedProcess) (bool, error) {
	if backend == nil || backend.exited() {
		return false, nil
	}
	if err := backend.process.Signal(terminationSignal()); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return false, nil
		}
		killErr := backend.process.Kill()
		waitErr := s.waitForForcedStop(backend)
		return true, errors.Join(fmt.Errorf("send SIGTERM to achievement backend: %w", err), killErr, waitErr)
	}

	timer := time.NewTimer(s.config.ShutdownTimeout)
	defer timer.Stop()
	select {
	case <-backend.done:
		return false, nil
	case <-timer.C:
		if err := backend.process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return true, fmt.Errorf("force-stop achievement backend: %w", err)
		}
		return true, s.waitForForcedStop(backend)
	}
}

func (s *Supervisor) waitForForcedStop(backend *trackedProcess) error {
	timer := time.NewTimer(s.config.ShutdownTimeout)
	defer timer.Stop()
	select {
	case <-backend.done:
		return nil
	case <-timer.C:
		return errors.New("achievement backend did not exit after force-stop")
	}
}

func (s *Supervisor) recordBackendFailure(result *Result, err error) {
	if err == nil {
		return
	}
	result.Degraded = true
	result.LastBackendError = err.Error()
	fmt.Fprintf(s.config.Backend.Output, "Selene: achievement backend unavailable: %v\n", err)
}

func (s *Supervisor) validate() error {
	if s.starter == nil || s.health == nil || s.steamRunning == nil || s.now == nil {
		return errors.New("achievement supervisor dependencies are incomplete")
	}
	if s.config.Backend.Path == "" || s.config.Steam.Path == "" {
		return errors.New("achievement supervisor process paths are required")
	}
	if s.config.ReadyTimeout <= 0 || s.config.SteamStartTimeout <= 0 || s.config.SteamExitGrace <= 0 || s.config.PollInterval <= 0 ||
		s.config.RestartBackoff < 0 || s.config.ShutdownTimeout <= 0 {
		return errors.New("achievement supervisor timeouts are invalid")
	}
	parsed, err := url.Parse(s.config.HealthURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" {
		return fmt.Errorf("invalid achievement health URL %q", s.config.HealthURL)
	}
	ip := net.ParseIP(parsed.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("achievement health URL must use a loopback IP: %s", parsed.Hostname())
	}
	return nil
}

func defaultHealthChecker() HealthChecker {
	client := &http.Client{Timeout: time.Second}
	return func(ctx context.Context, endpoint string) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("health endpoint returned %s", response.Status)
		}
		return nil
	}
}

func controlledEnvironment(env planner.Environment) []string {
	allow := map[string]bool{
		"LANG": true, "LANGUAGE": true, "LC_ALL": true, "LC_MESSAGES": true,
		"TERM": true, "COLORTERM": true, "NO_COLOR": true,
		"DISPLAY": true, "WAYLAND_DISPLAY": true,
		"XDG_RUNTIME_DIR": true, "DBUS_SESSION_BUS_ADDRESS": true,
		"USER": true, "LOGNAME": true, "SHELL": true,
	}
	values := make([]string, 0, len(allow)+10)
	for _, value := range os.Environ() {
		key, _, ok := strings.Cut(value, "=")
		if !ok || key == achievementserver.InternalModeEnv || key == achievementserver.HTTPAddressEnv ||
			(!allow[key] && !strings.HasPrefix(key, "LC_")) {
			continue
		}
		values = append(values, value)
	}
	return append(values,
		"HOME="+env.Home,
		"XDG_DATA_HOME="+env.XDGDataHome,
		"XDG_CACHE_HOME="+env.XDGCacheHome,
		"XDG_CONFIG_HOME="+env.XDGConfigHome,
		"XDG_STATE_HOME="+env.XDGStateHome,
		"PATH="+trustedPath,
	)
}

func requireExecutable(path, label string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("locate %s at %s: %w", label, path, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s is not executable: %s", label, path)
	}
	return nil
}

type execStarter struct{}

type execProcess struct {
	command *exec.Cmd
}

func (p *execProcess) Wait() error {
	return p.command.Wait()
}

func (p *execProcess) Signal(signal os.Signal) error {
	return p.command.Process.Signal(signal)
}

func (p *execProcess) Kill() error {
	return p.command.Process.Kill()
}

func (execStarter) Start(spec ProcessSpec) (Process, error) {
	command := exec.Command(spec.Path, spec.Args...)
	command.Env = append([]string(nil), spec.Env...)
	command.Stdout = spec.Output
	command.Stderr = spec.Output
	configureChildProcess(command, spec.TerminateWithParent)
	if err := command.Start(); err != nil {
		return nil, err
	}
	return &execProcess{command: command}, nil
}
