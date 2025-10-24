package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/AvengeMedia/danklinux/internal/log"
	"github.com/AvengeMedia/danklinux/internal/server"
)

func locateDMSConfig() (string, error) {
	var searchPaths []string

	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		if homeDir, err := os.UserHomeDir(); err == nil {
			configHome = filepath.Join(homeDir, ".config")
		}
	}

	if configHome != "" {
		searchPaths = append(searchPaths, filepath.Join(configHome, "quickshell", "dms"))
	}

	searchPaths = append(searchPaths, "/usr/share/quickshell/dms")

	configDirs := os.Getenv("XDG_CONFIG_DIRS")
	if configDirs == "" {
		configDirs = "/etc/xdg"
	}

	for _, dir := range strings.Split(configDirs, ":") {
		if dir != "" {
			searchPaths = append(searchPaths, filepath.Join(dir, "quickshell", "dms"))
		}
	}

	for _, path := range searchPaths {
		shellPath := filepath.Join(path, "shell.qml")
		if info, err := os.Stat(shellPath); err == nil && !info.IsDir() {
			return path, nil
		}
	}

	return "", fmt.Errorf("could not find DMS config (shell.qml) in any valid config path")
}

func getRuntimeDir() string {
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		return runtime
	}
	return os.TempDir()
}

func getPIDFilePath() string {
	return filepath.Join(getRuntimeDir(), fmt.Sprintf("danklinux-%d.pid", os.Getpid()))
}

func writePIDFile(childPID int) error {
	pidFile := getPIDFilePath()
	return os.WriteFile(pidFile, []byte(strconv.Itoa(childPID)), 0644)
}

func removePIDFile() {
	pidFile := getPIDFilePath()
	os.Remove(pidFile)
}

func getAllDMSPIDs() []int {
	dir := getRuntimeDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var pids []int

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "danklinux-") || !strings.HasSuffix(entry.Name(), ".pid") {
			continue
		}

		pidFile := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(pidFile)
		if err != nil {
			continue
		}

		childPID, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			os.Remove(pidFile)
			continue
		}

		// Check if the child process is still alive
		proc, err := os.FindProcess(childPID)
		if err != nil {
			os.Remove(pidFile)
			continue
		}

		if err := proc.Signal(syscall.Signal(0)); err != nil {
			// Process is dead, remove stale PID file
			os.Remove(pidFile)
			continue
		}

		pids = append(pids, childPID)

		// Also get the parent PID from the filename
		parentPIDStr := strings.TrimPrefix(entry.Name(), "danklinux-")
		parentPIDStr = strings.TrimSuffix(parentPIDStr, ".pid")
		if parentPID, err := strconv.Atoi(parentPIDStr); err == nil {
			// Check if parent is still alive
			if parentProc, err := os.FindProcess(parentPID); err == nil {
				if err := parentProc.Signal(syscall.Signal(0)); err == nil {
					pids = append(pids, parentPID)
				}
			}
		}
	}

	return pids
}

func runShellInteractive() {
	go printASCII()
	fmt.Fprintf(os.Stderr, "dms %s\n", Version)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	socketPath := server.GetSocketPath()

	errChan := make(chan error, 2)

	go func() {
		if err := server.Start(false); err != nil {
			errChan <- fmt.Errorf("server error: %w", err)
		}
	}()

	configPath, err := locateDMSConfig()
	if err != nil {
		log.Fatalf("Error locating DMS config: %v", err)
	}

	log.Infof("Spawning quickshell with -p %s", configPath)

	cmd := exec.CommandContext(ctx, "qs", "-p", configPath)
	cmd.Env = append(os.Environ(), "DMS_SOCKET="+socketPath)
	if qtRules := log.GetQtLoggingRules(); qtRules != "" {
		cmd.Env = append(cmd.Env, "QT_LOGGING_RULES="+qtRules)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Fatalf("Error starting quickshell: %v", err)
	}

	// Write PID file for the quickshell child process
	if err := writePIDFile(cmd.Process.Pid); err != nil {
		log.Warnf("Failed to write PID file: %v", err)
	}
	defer removePIDFile()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := cmd.Wait(); err != nil {
			errChan <- fmt.Errorf("quickshell exited: %w", err)
		} else {
			errChan <- fmt.Errorf("quickshell exited")
		}
	}()

	select {
	case sig := <-sigChan:
		log.Infof("\nReceived signal %v, shutting down...", sig)
		cancel()
		cmd.Process.Kill()
		os.Remove(socketPath)
	case err := <-errChan:
		log.Error(err)
		cancel()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		os.Remove(socketPath)
		os.Exit(1)
	}
}

func restartShell() {
	killShell()
	runShellDaemon()
}

func killShell() {
	// Get all tracked DMS PIDs from PID files
	pids := getAllDMSPIDs()

	if len(pids) == 0 {
		log.Info("No running DMS shell instances found.")
		return
	}

	currentPid := os.Getpid()
	uniquePids := make(map[int]bool)

	// Deduplicate and filter out current process
	for _, pid := range pids {
		if pid != currentPid {
			uniquePids[pid] = true
		}
	}

	// Kill all tracked processes
	for pid := range uniquePids {
		proc, err := os.FindProcess(pid)
		if err != nil {
			log.Errorf("Error finding process %d: %v", pid, err)
			continue
		}

		if err := proc.Kill(); err != nil {
			log.Errorf("Error killing process %d: %v", pid, err)
		} else {
			log.Infof("Killed DMS process with PID %d", pid)
		}
	}

	// Clean up any remaining PID files
	dir := getRuntimeDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "danklinux-") && strings.HasSuffix(entry.Name(), ".pid") {
			pidFile := filepath.Join(dir, entry.Name())
			os.Remove(pidFile)
		}
	}
}

func runShellDaemon() {
	// Check if this is the daemon child process by looking for the hidden flag
	isDaemonChild := false
	for _, arg := range os.Args {
		if arg == "--daemon-child" {
			isDaemonChild = true
			break
		}
	}

	if !isDaemonChild {
		fmt.Fprintf(os.Stderr, "dms %s\n", Version)

		cmd := exec.Command(os.Args[0], "run", "-d", "--daemon-child")
		cmd.Env = os.Environ()

		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}

		if err := cmd.Start(); err != nil {
			log.Fatalf("Error starting daemon: %v", err)
		}

		log.Infof("DMS shell daemon started (PID: %d)", cmd.Process.Pid)
		return
	}

	fmt.Fprintf(os.Stderr, "dms %s\n", Version)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	socketPath := server.GetSocketPath()

	errChan := make(chan error, 2)

	go func() {
		if err := server.Start(false); err != nil {
			errChan <- fmt.Errorf("server error: %w", err)
		}
	}()

	configPath, err := locateDMSConfig()
	if err != nil {
		log.Fatalf("Error locating DMS config: %v", err)
	}

	log.Infof("Spawning quickshell with -p %s", configPath)

	cmd := exec.CommandContext(ctx, "qs", "-p", configPath)
	cmd.Env = append(os.Environ(), "DMS_SOCKET="+socketPath)
	if qtRules := log.GetQtLoggingRules(); qtRules != "" {
		cmd.Env = append(cmd.Env, "QT_LOGGING_RULES="+qtRules)
	}

	devNull, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		log.Fatalf("Error opening /dev/null: %v", err)
	}
	defer devNull.Close()

	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull

	if err := cmd.Start(); err != nil {
		log.Fatalf("Error starting daemon: %v", err)
	}

	// Write PID file for the quickshell child process
	if err := writePIDFile(cmd.Process.Pid); err != nil {
		log.Warnf("Failed to write PID file: %v", err)
	}
	defer removePIDFile()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := cmd.Wait(); err != nil {
			errChan <- fmt.Errorf("quickshell exited: %w", err)
		} else {
			errChan <- fmt.Errorf("quickshell exited")
		}
	}()

	select {
	case <-sigChan:
		cancel()
		cmd.Process.Kill()
		os.Remove(socketPath)
	case <-errChan:
		cancel()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		os.Remove(socketPath)
		os.Exit(1)
	}
}

func runShellIPCCommand(args []string) {
	if len(args) == 0 {
		log.Error("IPC command requires arguments")
		log.Info("Usage: dms ipc <command> [args...]")
		os.Exit(1)
	}

	if args[0] != "call" {
		args = append([]string{"call"}, args...)
	}

	configPath, err := locateDMSConfig()
	if err != nil {
		log.Fatalf("Error locating DMS config: %v", err)
	}

	cmdArgs := append([]string{"-p", configPath, "ipc"}, args...)
	cmd := exec.Command("qs", cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("Error running IPC command: %v", err)
	}
}
