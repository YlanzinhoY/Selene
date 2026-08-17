package plugins

// steamRunningCheck is the operation safety boundary. Production uses the
// real process table; tests replace only this read-only check while exercising
// filesystem transactions inside t.TempDir.
var steamRunningCheck = SteamRunning

func steamRunning() bool {
	return steamRunningCheck()
}
