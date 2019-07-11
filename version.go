package main

// plugged in during build
var (
	SemVer    = "-"
	GitCommit = "-"
	GitTag    = "-"
)

// Version contains versioning information obtained during build
type Version struct {
	SemVer    string `json:"semVer"`
	GitCommit string `json:"gitCommit"`
	GitTag    string `json:"gitTag"`
}

// GetVersion provides version and build information
func GetVersion() Version {
	return Version{SemVer: SemVer, GitCommit: GitCommit, GitTag: GitTag}
}
