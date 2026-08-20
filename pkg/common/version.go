package common

import "runtime/debug"

// modulePath identifies vulkan in a consumer binary's dependency list.
const modulePath = "github.com/agentstax/vulkan"

// BuildVersion reads vulkan's module version from the binary's build info:
// the dependency's version when vulkan is imported, the main module's when
// built from this repo, "unknown" when the binary carries no build info.
func BuildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	if info.Main.Path == modulePath {
		return info.Main.Version
	}
	for _, dependency := range info.Deps {
		if dependency.Path == modulePath {
			return dependency.Version
		}
	}
	return "unknown"
}
