package version

// Version is set at link time from the release tag (e.g. 0.1.0).
var Version = "dev"

func String() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
