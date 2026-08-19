//go:build linux && !cgo

package notifier

// PlaySound is intentionally disabled in Selene's static Linux build because
// oto's Linux output driver requires CGO. Visual DBus notifications continue to
// work, and builds with CGO enabled use the full audio implementation.
func (s *Service) PlaySound(filename string) error {
	return nil
}
