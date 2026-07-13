package version

import "strings"

// tag holds the build-time client version tag, injected via:
//
//	-ldflags "-X github.com/yarma/tsession/internal/version.tag=vX.Y.Z"
var tag = ""

// CurrentTag returns the build-time client tag (e.g. "v1.2.3"), or an empty
// string when the binary was not built with a version tag.
func CurrentTag() string {
	return strings.TrimSpace(tag)
}
