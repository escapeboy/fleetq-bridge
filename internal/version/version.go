package version

// These are set by the Go linker via -ldflags at build time.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns the full version string.
func String() string {
	return Version + " (" + Commit + ") built " + Date
}
