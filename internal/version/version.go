package version

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type VersionInfo struct {
	Current   string
	Latest    string
	IsGit     bool
	IsBranch  bool
	IsTag     bool
	HasUpdate bool
}

func GetCurrentDMSVersion() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	dmsPath := filepath.Join(homeDir, ".config", "quickshell", "dms")
	if _, err := os.Stat(dmsPath); os.IsNotExist(err) {
		return "", fmt.Errorf("DMS not installed")
	}

	originalDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(dmsPath); err != nil {
		return "", fmt.Errorf("failed to change to DMS directory: %w", err)
	}

	if _, err := os.Stat(filepath.Join(dmsPath, ".git")); err == nil {
		tagCmd := exec.Command("git", "describe", "--exact-match", "--tags", "HEAD")
		if tagOutput, err := tagCmd.Output(); err == nil {
			return strings.TrimSpace(string(tagOutput)), nil
		}

		branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		if branchOutput, err := branchCmd.Output(); err == nil {
			branch := strings.TrimSpace(string(branchOutput))
			revCmd := exec.Command("git", "rev-parse", "--short", "HEAD")
			if revOutput, err := revCmd.Output(); err == nil {
				rev := strings.TrimSpace(string(revOutput))
				return fmt.Sprintf("%s@%s", branch, rev), nil
			}
			return branch, nil
		}
	}

	cmd := exec.Command("dms", "--version")
	if output, err := cmd.Output(); err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	return "unknown", nil
}

func GetLatestDMSVersion() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	dmsPath := filepath.Join(homeDir, ".config", "quickshell", "dms")

	originalDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	defer os.Chdir(originalDir)

	if _, err := os.Stat(filepath.Join(dmsPath, ".git")); err == nil {
		if err := os.Chdir(dmsPath); err != nil {
			return "", fmt.Errorf("failed to change to DMS directory: %w", err)
		}

		currentRefCmd := exec.Command("git", "symbolic-ref", "-q", "HEAD")
		currentRefOutput, _ := currentRefCmd.Output()
		onBranch := len(currentRefOutput) > 0

		if !onBranch {
			tagCmd := exec.Command("git", "describe", "--exact-match", "--tags", "HEAD")
			if _, err := tagCmd.Output(); err == nil {
				// Add timeout to git fetch to prevent hanging
				fetchCmd := exec.Command("timeout", "5s", "git", "fetch", "origin", "--tags", "--quiet")
				fetchCmd.Run()

				latestTagCmd := exec.Command("git", "tag", "-l", "v0.1.*", "--sort=-version:refname")
				latestTagOutput, err := latestTagCmd.Output()
				if err != nil {
					return "", fmt.Errorf("failed to get latest tag: %w", err)
				}

				tags := strings.Split(strings.TrimSpace(string(latestTagOutput)), "\n")
				if len(tags) == 0 || tags[0] == "" {
					return "", fmt.Errorf("no v0.1.* tags found")
				}
				return tags[0], nil
			}
		} else {
			branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
			branchOutput, err := branchCmd.Output()
			if err != nil {
				return "", fmt.Errorf("failed to get current branch: %w", err)
			}
			currentBranch := strings.TrimSpace(string(branchOutput))

			// Add timeout to git fetch to prevent hanging
			fetchCmd := exec.Command("timeout", "5s", "git", "fetch", "origin", currentBranch, "--quiet")
			fetchCmd.Run()

			remoteRevCmd := exec.Command("git", "rev-parse", "--short", fmt.Sprintf("origin/%s", currentBranch))
			remoteRevOutput, err := remoteRevCmd.Output()
			if err != nil {
				return "", fmt.Errorf("failed to get remote revision: %w", err)
			}
			remoteRev := strings.TrimSpace(string(remoteRevOutput))
			return fmt.Sprintf("%s@%s", currentBranch, remoteRev), nil
		}
	}

	// Add timeout to prevent hanging when GitHub is down
	cmd := exec.Command("curl", "-s", "--max-time", "5", "https://api.github.com/repos/AvengeMedia/danklinux/releases/latest")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest release: %w", err)
	}

	var result struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		for _, line := range strings.Split(string(output), "\n") {
			if strings.Contains(line, "\"tag_name\"") {
				parts := strings.Split(line, "\"")
				if len(parts) >= 4 {
					return parts[3], nil
				}
			}
		}
		return "", fmt.Errorf("failed to parse latest version: %w", err)
	}

	return result.TagName, nil
}

func GetDMSVersionInfo() (*VersionInfo, error) {
	current, err := GetCurrentDMSVersion()
	if err != nil {
		return nil, err
	}

	latest, err := GetLatestDMSVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get latest version: %w", err)
	}

	info := &VersionInfo{
		Current:  current,
		Latest:   latest,
		IsGit:    strings.Contains(current, "@"),
		IsBranch: strings.Contains(current, "@"),
		IsTag:    !strings.Contains(current, "@") && strings.HasPrefix(current, "v"),
	}

	if info.IsBranch {
		parts := strings.Split(current, "@")
		latestParts := strings.Split(latest, "@")
		if len(parts) == 2 && len(latestParts) == 2 {
			info.HasUpdate = parts[1] != latestParts[1]
		}
	} else if info.IsTag {
		info.HasUpdate = current != latest
	} else {
		info.HasUpdate = false
	}

	return info, nil
}

func CompareVersions(v1, v2 string) int {
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		var p1, p2 int
		if i < len(parts1) {
			fmt.Sscanf(parts1[i], "%d", &p1)
		}
		if i < len(parts2) {
			fmt.Sscanf(parts2[i], "%d", &p2)
		}

		if p1 < p2 {
			return -1
		}
		if p1 > p2 {
			return 1
		}
	}

	return 0
}
