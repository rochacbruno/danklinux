package osinfo

import (
	"bufio"
	"os"
	"strings"

	"github.com/AvengeMedia/dankinstall/internal/errdefs"
)

var osOpen = os.Open

func detectLinuxDistro(info *OSInfo) error {
	if err := readOSRelease(info); err == nil {
		return nil
	}

	return errdefs.NewCustomError(errdefs.ErrTypeGeneric, "Failed to detect Linux distribution")
}

func readOSRelease(info *OSInfo) error {
	file, err := osOpen("/etc/os-release")
	if err != nil {
		return err
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
			if value == "arch" {
				info.Distribution = "arch"
				info.VersionID = "rolling"
			} else {
				info.Distribution = value
			}
		case "VERSION_ID":
			if info.Distribution != "arch" {
				info.VersionID = value
			}
		case "VERSION":
			info.Version = value
		case "PRETTY_NAME":
			info.PrettyName = value
		}
	}

	return scanner.Err()
}