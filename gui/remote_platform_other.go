//go:build !windows

package main

func remotePTYCapability() (bool, string) {
	return false, "Windows ConPTY is only available on Windows"
}

func remotePTYInteractiveSmokeProbe() (bool, string) {
	return false, "ConPTY interactive probe is only available on Windows"
}

func remoteClaudeLaunchSmokeProbe(cmd CommandSpec) (bool, string) {
	_ = cmd
	return false, "Claude launch probe is only available on Windows"
}
