package remote

import (
	"os"
	goruntime "runtime"
)

const (
	DefaultRemoteHeartbeatSec = 5
	MinRemoteHeartbeatSec     = 5
)

// MachineProfile describes the local machine for hub registration.
type MachineProfile struct {
	Name           string
	Platform       string
	Hostname       string
	Arch           string
	AppVersion     string
	HeartbeatSec   int
	ActiveSessions int
}

// NormalizeHeartbeatIntervalSec clamps the heartbeat interval to the minimum.
func NormalizeHeartbeatIntervalSec(value int) int {
	if value < MinRemoteHeartbeatSec {
		return DefaultRemoteHeartbeatSec
	}
	return value
}

// RemoteAppVersion returns the current app version string.
func RemoteAppVersion() string {
	return "1.0.0"
}

// NormalizedRemotePlatform returns a normalized platform string.
func NormalizedRemotePlatform() string {
	switch goruntime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "mac"
	case "linux":
		return "linux"
	default:
		return "linux"
	}
}

// CurrentMachineProfile builds a MachineProfile for the local machine.
func CurrentMachineProfile(heartbeatSec int, activeSessions int) MachineProfile {
	name := "MaClaw Desktop"
	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		name = hostname
	}
	return MachineProfile{
		Name:           name,
		Platform:       NormalizedRemotePlatform(),
		Hostname:       hostname,
		Arch:           goruntime.GOARCH,
		AppVersion:     RemoteAppVersion(),
		HeartbeatSec:   NormalizeHeartbeatIntervalSec(heartbeatSec),
		ActiveSessions: activeSessions,
	}
}
