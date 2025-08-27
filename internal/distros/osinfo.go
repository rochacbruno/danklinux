package distros

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/AvengeMedia/dankinstall/internal/errdefs"
)

// DistroInfo contains basic information about a distribution
type DistroInfo struct {
	ID           string
	HexColorCode string
}

// OSInfo contains complete OS information
type OSInfo struct {
	Distribution DistroInfo
	Version      string
	VersionID    string
	PrettyName   string
	Architecture string
}

// GetSupportedDistros returns all supported distributions by querying the registry
func GetSupportedDistros() []DistroInfo {
	var distros []DistroInfo
	for id, config := range Registry {
		distros = append(distros, DistroInfo{
			ID:           id,
			HexColorCode: config.ColorHex,
		})
	}
	return distros
}

// GetOSInfo detects the current OS and returns information about it
func GetOSInfo() (*OSInfo, error) {
	if runtime.GOOS != "linux" {
		return nil, errdefs.NewCustomError(errdefs.ErrTypeNotLinux, fmt.Sprintf("Only linux is supported, but I found %s", runtime.GOOS))
	}

	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return nil, errdefs.NewCustomError(errdefs.ErrTypeInvalidArchitecture, fmt.Sprintf("Only amd64 and arm64 are supported, but I found %s", runtime.GOARCH))
	}

	info := &OSInfo{
		Architecture: runtime.GOARCH,
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
			// Check if we support this distribution
			config, exists := Registry[value]
			if !exists {
				return nil, errdefs.NewCustomError(errdefs.ErrTypeUnsupportedDistribution, fmt.Sprintf("Unsupported distribution: %s", value))
			}
			
			// Get distribution info from the registry
			info.Distribution = DistroInfo{
				ID:           value, // Use the actual ID from os-release
				HexColorCode: config.ColorHex,
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
	config, exists := Registry[distroID]
	if !exists {
		return nil, fmt.Errorf("unsupported distribution: %s", distroID)
	}
	
	return &DistroInfo{
		ID:           distroID,
		HexColorCode: config.ColorHex,
	}, nil
}

// IsUnsupportedDistro checks if a distribution/version combination is supported
func IsUnsupportedDistro(distroID, versionID string) bool {
	switch distroID {
	case "arch", "cachyos", "fedora":
		return false // These are supported
	case "ubuntu":
		// Parse version (format: "24.04")
		parts := strings.Split(versionID, ".")
		if len(parts) >= 2 {
			major, err1 := strconv.Atoi(parts[0])
			minor, err2 := strconv.Atoi(parts[1])
			
			if err1 == nil && err2 == nil {
				// Check if version is less than 25.04
				return major < 25 || (major == 25 && minor < 4)
			}
		}
		return true // Unknown Ubuntu version format
	default:
		return true // Unsupported distro
	}
}