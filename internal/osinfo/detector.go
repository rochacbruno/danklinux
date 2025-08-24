package osinfo

import (
	"fmt"
	"runtime"
	"slices"

	"github.com/AvengeMedia/dankinstall/internal/errdefs"
)

var AllSupportedDistros = []string{
	"arch",
	"fedora",
}

var AllSupportedDistrosVersions = map[string][]string{
	"arch": {
		"rolling",
	},
	"fedora": {
		"40",
		"41",
		"42",
	},
}

type OSInfo struct {
	Distribution string
	Version      string
	VersionID    string
	PrettyName   string
	Architecture string
}

var getOsFunc = getGoos
var getArchFunc = getGoarch

func getGoos() string {
	return runtime.GOOS
}

func getGoarch() string {
	return runtime.GOARCH
}

func IsVersionSupported(distribution, version string) bool {
	versions, ok := AllSupportedDistrosVersions[distribution]
	if !ok {
		return false
	}

	if distribution == "arch" && version == "rolling" {
		return true
	}

	return slices.Contains(versions, version)
}

func GetOSInfo() (*OSInfo, error) {
	if getOsFunc() != "linux" {
		return nil, errdefs.NewCustomError(errdefs.ErrTypeNotLinux, fmt.Sprintf("Only linux is supported, but I found %s", runtime.GOOS))
	}

	if getGoarch() != "amd64" && getGoarch() != "arm64" {
		return nil, errdefs.NewCustomError(errdefs.ErrTypeInvalidArchitecture, fmt.Sprintf("Only amd64 and arm64 are supported, but I found %s", runtime.GOARCH))
	}

	info := &OSInfo{
		Architecture: getArchFunc(),
	}

	err := detectLinuxDistro(info)
	if err != nil {
		return nil, err
	}

	if !slices.Contains(AllSupportedDistros, info.Distribution) {
		return nil, errdefs.NewCustomError(errdefs.ErrTypeUnsupportedDistribution, fmt.Sprintf("Unsupported distribution: %s", info.Distribution))
	}

	if !IsVersionSupported(info.Distribution, info.VersionID) {
		return nil, errdefs.NewCustomError(errdefs.ErrTypeUnsupportedVersion, fmt.Sprintf("Unsupported version: %s", info.VersionID))
	}

	return info, nil
}