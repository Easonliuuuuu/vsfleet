// Package version carries the build identity, stamped by the release build.
package version

import "runtime/debug"

// Version is the release version, overridden at link time.
var Version = "dev"

// Commit is the source revision, overridden at link time.
var Commit = ""

// Date is the build date, overridden at link time.
var Date = ""

// String renders the version for --version and the vCenter user agent.
func String() string {
	v := Version
	if c := commit(); c != "" {
		v += " (" + c + ")"
	}
	return v
}

// UserAgent identifies vcfleet to vCenter. Operators reading vCenter's session
// list should be able to tell what connected.
func UserAgent() string { return "vcfleet/" + Version }

func commit() string {
	if Commit != "" {
		return Commit
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return s.Value[:7]
		}
	}
	return ""
}
