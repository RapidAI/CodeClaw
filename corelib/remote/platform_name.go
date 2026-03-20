package remote

import goruntime "runtime"

// PlatformGOOS returns the current GOOS value. It is a variable to allow
// test overrides.
var PlatformGOOS = func() string {
	return goruntime.GOOS
}
