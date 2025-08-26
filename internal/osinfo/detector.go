package osinfo

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/AvengeMedia/dankinstall/internal/errdefs"
)

var AllSupportedDistros = []DistroInfo{
	{ID: "arch", HexColorCode: "#1793D1", DetectorType: "arch", InstallerType: "arch"},
	{ID: "fedora", HexColorCode: "#0B57A4", DetectorType: "fedora", InstallerType: "fedora"},
	{ID: "cachyos", HexColorCode: "#1793D1", DetectorType: "arch", InstallerType: "arch"}, // Uses Arch implementations
}

type DistroInfo struct {
	ID            string
	HexColorCode  string
	DetectorType  string // Which detector implementation to use
	InstallerType string // Which installer implementation to use
}

type OSInfo struct {
	Distribution DistroInfo
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

	file, err := os.Open("/etc/os-release")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := strings.Trim(parts[1], "\"")

		switch key {
		case "ID":
			if !slices.ContainsFunc(AllSupportedDistros, func(d DistroInfo) bool {
				return d.ID == value
			}) {
				return nil, errdefs.NewCustomError(errdefs.ErrTypeUnsupportedDistribution, fmt.Sprintf("Unsupported distribution: %s", value))
			}
			for _, d := range AllSupportedDistros {
				if d.ID == value {
					info.Distribution = d
					break
				}
			}
		case "VERSION_ID", "BUILD_ID":
			info.VersionID = value
		case "VERSION":
			info.Version = value
		case "PRETTY_NAME":
			info.PrettyName = value
		}
	}

	return info, scanner.Err()
}

// GetDistroInfo returns the DistroInfo for a given distribution ID
func GetDistroInfo(distroID string) (*DistroInfo, error) {
	for _, d := range AllSupportedDistros {
		if d.ID == distroID {
			return &d, nil
		}
	}
	return nil, fmt.Errorf("unsupported distribution: %s", distroID)
}
