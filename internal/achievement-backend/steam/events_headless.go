package steam

// Fetch progress is exposed through LibrarySyncStatus in the HTTP API.
func (s *Service) emitFetchStatus(current uint32, total uint32) {}
