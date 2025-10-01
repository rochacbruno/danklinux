package greeter

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AvengeMedia/dankinstall/internal/distros"
)

// DetectDMSPath checks for DMS installation in user config and system config
func DetectDMSPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	userPath := filepath.Join(homeDir, ".config", "quickshell", "dms")
	if info, err := os.Stat(userPath); err == nil && info.IsDir() {
		return userPath, nil
	}

	systemPath := "/etc/xdg/quickshell/dms"
	if info, err := os.Stat(systemPath); err == nil && info.IsDir() {
		return systemPath, nil
	}

	return "", fmt.Errorf("couldn't find dms installation")
}

// DetectCompositors checks which compositors are installed
func DetectCompositors() []string {
	var compositors []string

	if commandExists("niri") {
		compositors = append(compositors, "niri")
	}
	if commandExists("Hyprland") {
		compositors = append(compositors, "Hyprland")
	}

	return compositors
}

// PromptCompositorChoice asks user to choose between compositors
func PromptCompositorChoice(compositors []string) (string, error) {
	fmt.Println("\nMultiple compositors detected:")
	for i, comp := range compositors {
		fmt.Printf("%d) %s\n", i+1, comp)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Choose compositor for greeter (1-2): ")
	response, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("error reading input: %w", err)
	}

	response = strings.TrimSpace(response)
	switch response {
	case "1":
		return compositors[0], nil
	case "2":
		if len(compositors) > 1 {
			return compositors[1], nil
		}
		return "", fmt.Errorf("invalid choice")
	default:
		return "", fmt.Errorf("invalid choice")
	}
}

// EnsureGreetdInstalled checks if greetd is installed and installs it if not
func EnsureGreetdInstalled(logFunc func(string), sudoPassword string) error {
	if commandExists("greetd") {
		logFunc("✓ greetd is already installed")
		return nil
	}

	logFunc("greetd is not installed. Installing...")

	osInfo, err := distros.GetOSInfo()
	if err != nil {
		return fmt.Errorf("failed to detect OS: %w", err)
	}

	ctx := context.Background()
	var installCmd *exec.Cmd

	switch strings.ToLower(osInfo.Distribution.ID) {
	case "arch", "cachyos", "endeavouros", "manjaro":
		if sudoPassword != "" {
			installCmd = exec.CommandContext(ctx, "bash", "-c",
				fmt.Sprintf("echo '%s' | sudo -S pacman -S --needed --noconfirm greetd", sudoPassword))
		} else {
			installCmd = exec.CommandContext(ctx, "sudo", "pacman", "-S", "--needed", "--noconfirm", "greetd")
		}

	case "fedora":
		if sudoPassword != "" {
			installCmd = exec.CommandContext(ctx, "bash", "-c",
				fmt.Sprintf("echo '%s' | sudo -S dnf install -y greetd", sudoPassword))
		} else {
			installCmd = exec.CommandContext(ctx, "sudo", "dnf", "install", "-y", "greetd")
		}

	case "ubuntu", "debian":
		if sudoPassword != "" {
			installCmd = exec.CommandContext(ctx, "bash", "-c",
				fmt.Sprintf("echo '%s' | sudo -S apt-get install -y greetd", sudoPassword))
		} else {
			installCmd = exec.CommandContext(ctx, "sudo", "apt-get", "install", "-y", "greetd")
		}

	case "opensuse", "opensuse-tumbleweed", "opensuse-leap":
		if sudoPassword != "" {
			installCmd = exec.CommandContext(ctx, "bash", "-c",
				fmt.Sprintf("echo '%s' | sudo -S zypper install -y greetd", sudoPassword))
		} else {
			installCmd = exec.CommandContext(ctx, "sudo", "zypper", "install", "-y", "greetd")
		}

	case "nixos":
		return fmt.Errorf("on NixOS, please add greetd to your configuration.nix")

	default:
		return fmt.Errorf("unsupported distribution for automatic greetd installation: %s", osInfo.Distribution.ID)
	}

	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr

	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("failed to install greetd: %w", err)
	}

	logFunc("✓ greetd installed successfully")
	return nil
}

// CopyGreeterFiles copies the appropriate greeter files based on compositor
func CopyGreeterFiles(dmsPath, compositor string, logFunc func(string), sudoPassword string) error {
	assetsDir := filepath.Join(dmsPath, "Modules", "Greetd", "assets")

	if _, err := os.Stat(assetsDir); os.IsNotExist(err) {
		return fmt.Errorf("greeter assets not found at %s", assetsDir)
	}

	var configSrc, scriptSrc string
	var configDst string

	switch strings.ToLower(compositor) {
	case "niri":
		configSrc = filepath.Join(assetsDir, "dms-niri.kdl")
		scriptSrc = filepath.Join(assetsDir, "greet-niri.sh")
		configDst = "/etc/greetd/dms-niri.kdl"
	case "hyprland":
		configSrc = filepath.Join(assetsDir, "dms-hypr.conf")
		scriptSrc = filepath.Join(assetsDir, "greet-hyprland.sh")
		configDst = "/etc/greetd/dms-hypr.conf"
	default:
		return fmt.Errorf("unsupported compositor: %s", compositor)
	}

	if err := runSudoCmd(sudoPassword, "mkdir", "-p", "/etc/greetd"); err != nil {
		return fmt.Errorf("failed to create /etc/greetd: %w", err)
	}

	if err := runSudoCmd(sudoPassword, "cp", configSrc, configDst); err != nil {
		return fmt.Errorf("failed to copy config file: %w", err)
	}
	logFunc(fmt.Sprintf("✓ Copied %s to %s", filepath.Base(configSrc), configDst))

	scriptDst := "/etc/greetd/start-dms.sh"
	if err := runSudoCmd(sudoPassword, "cp", scriptSrc, scriptDst); err != nil {
		return fmt.Errorf("failed to copy script file: %w", err)
	}
	logFunc(fmt.Sprintf("✓ Copied %s to %s", filepath.Base(scriptSrc), scriptDst))

	if err := runSudoCmd(sudoPassword, "chmod", "+x", scriptDst); err != nil {
		return fmt.Errorf("failed to make script executable: %w", err)
	}

	sedCmd := fmt.Sprintf("s|_DMS_PATH_|%s|g", dmsPath)
	if err := runSudoCmd(sudoPassword, "sed", "-i", sedCmd, configDst); err != nil {
		return fmt.Errorf("failed to update DMS path in config: %w", err)
	}
	logFunc(fmt.Sprintf("✓ Updated DMS path to %s", dmsPath))

	return nil
}

// SyncDMSConfigs creates symlinks to sync DMS configs to greetd
func SyncDMSConfigs(logFunc func(string), sudoPassword string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	greeterUser, err := getGreeterUser()
	if err != nil {
		return fmt.Errorf("failed to get greeter user: %w", err)
	}

	if err := runSudoCmd(sudoPassword, "mkdir", "-p", "/etc/greetd/.dms"); err != nil {
		return fmt.Errorf("failed to create /etc/greetd/.dms: %w", err)
	}

	if err := runSudoCmd(sudoPassword, "chown", "-R", greeterUser, "/etc/greetd/.dms"); err != nil {
		return fmt.Errorf("failed to chown /etc/greetd/.dms: %w", err)
	}
	logFunc("✓ Created /etc/greetd/.dms directory")

	symlinks := []struct {
		source string
		target string
		desc   string
	}{
		{
			source: filepath.Join(homeDir, ".config", "DankMaterialShell", "settings.json"),
			target: "/etc/greetd/.dms/settings.json",
			desc:   "core settings (theme, clock formats, etc)",
		},
		{
			source: filepath.Join(homeDir, ".local", "state", "DankMaterialShell", "session.json"),
			target: "/etc/greetd/.dms/session.json",
			desc:   "state (wallpaper configuration)",
		},
		{
			source: filepath.Join(homeDir, ".cache", "quickshell", "dankshell", "dms-colors.json"),
			target: "/etc/greetd/.dms/colors.json",
			desc:   "wallpaper based theming",
		},
	}

	for _, link := range symlinks {
		sourceDir := filepath.Dir(link.source)
		if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
			if err := os.MkdirAll(sourceDir, 0755); err != nil {
				logFunc(fmt.Sprintf("⚠ Warning: Could not create directory %s: %v", sourceDir, err))
				continue
			}
		}

		if _, err := os.Stat(link.source); os.IsNotExist(err) {
			if err := os.WriteFile(link.source, []byte("{}"), 0644); err != nil {
				logFunc(fmt.Sprintf("⚠ Warning: Could not create %s: %v", link.source, err))
				continue
			}
		}

		runSudoCmd(sudoPassword, "rm", "-f", link.target)

		if err := runSudoCmd(sudoPassword, "ln", "-sf", link.source, link.target); err != nil {
			logFunc(fmt.Sprintf("⚠ Warning: Failed to create symlink for %s: %v", link.desc, err))
			continue
		}

		logFunc(fmt.Sprintf("✓ Synced %s", link.desc))
	}

	return nil
}

// ConfigureGreetd configures the greetd config.toml file
func ConfigureGreetd(logFunc func(string), sudoPassword string) error {
	configPath := "/etc/greetd/config.toml"

	if _, err := os.Stat(configPath); err == nil {
		backupPath := configPath + ".backup"
		if err := runSudoCmd(sudoPassword, "cp", configPath, backupPath); err != nil {
			return fmt.Errorf("failed to backup config: %w", err)
		}
		logFunc(fmt.Sprintf("✓ Backed up existing config to %s", backupPath))
	}

	var configContent string
	if data, err := os.ReadFile(configPath); err == nil {
		configContent = string(data)
	} else {
		configContent = `[terminal]
# The VT to run the greeter on. Can be "next", "current" or a number
# designating the VT.
vt = 1

# The default session, also known as the greeter.
[default_session]

# The user to run the command as. The privileges this user must have depends
# on the greeter. A graphical greeter may for example require the user to be
# in the video group.
user = "greeter"
`
	}

	lines := strings.Split(configContent, "\n")
	var newLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "command =") && !strings.HasPrefix(trimmed, "command=") {
			newLines = append(newLines, line)
		}
	}

	var finalLines []string
	inDefaultSession := false
	commandAdded := false

	for _, line := range newLines {
		finalLines = append(finalLines, line)
		trimmed := strings.TrimSpace(line)

		if trimmed == "[default_session]" {
			inDefaultSession = true
		}

		if inDefaultSession && !commandAdded && trimmed != "" && !strings.HasPrefix(trimmed, "[") {
			if !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "user") {
				finalLines = append(finalLines, `command = "/etc/greetd/start-dms.sh"`)
				commandAdded = true
			}
		}
	}

	if !commandAdded {
		finalLines = append(finalLines, `command = "/etc/greetd/start-dms.sh"`)
	}

	newConfig := strings.Join(finalLines, "\n")

	tmpFile := "/tmp/greetd-config.toml"
	if err := os.WriteFile(tmpFile, []byte(newConfig), 0644); err != nil {
		return fmt.Errorf("failed to write temp config: %w", err)
	}

	if err := runSudoCmd(sudoPassword, "mv", tmpFile, configPath); err != nil {
		return fmt.Errorf("failed to move config to /etc/greetd: %w", err)
	}

	logFunc("✓ Updated greetd configuration")
	return nil
}

func runSudoCmd(sudoPassword string, command string, args ...string) error {
	var cmd *exec.Cmd

	if sudoPassword != "" {
		fullArgs := append([]string{command}, args...)
		quotedArgs := make([]string, len(fullArgs))
		for i, arg := range fullArgs {
			quotedArgs[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
		}
		cmdStr := strings.Join(quotedArgs, " ")

		cmd = exec.Command("bash", "-c", fmt.Sprintf("echo '%s' | sudo -S %s", sudoPassword, cmdStr))
	} else {
		cmd = exec.Command("sudo", append([]string{command}, args...)...)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func getGreeterUser() (string, error) {
	configPath := "/etc/greetd/config.toml"

	data, err := os.ReadFile(configPath)
	if err != nil {
		return "greeter", nil
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "user =") || strings.HasPrefix(trimmed, "user=") {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				user := strings.TrimSpace(parts[1])
				user = strings.Trim(user, `"'`)
				return user, nil
			}
		}
	}

	return "greeter", nil
}

func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}
