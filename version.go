// Copyright 2019 Koninklijke KPN N.V.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
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
