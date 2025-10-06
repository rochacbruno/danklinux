package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/AvengeMedia/danklinux/internal/log"
	"github.com/AvengeMedia/danklinux/internal/server"
)

func runShellInteractive() {
	printASCII()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	socketPath := server.GetSocketPath()

	errChan := make(chan error, 2)

	go func() {
		if err := server.Start(); err != nil {
			errChan <- fmt.Errorf("server error: %w", err)
		}
	}()

	cmd := exec.CommandContext(ctx, "qs", "-c", "dms")
	cmd.Env = append(os.Environ(), "DMS_SOCKET="+socketPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Fatalf("Error starting quickshell: %v", err)
	}

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
	// Find qs, quickshell processes that include "dms" or "DankMaterialShell" in their command line
	patterns := []string{
		"qs.*dms",
		"qs.*DankMaterialShell",
		"quickshell.*dms",
		"quickshell.*DankMaterialShell",
	}

	var allPids []string

	for _, pattern := range patterns {
		out, err := exec.Command("pgrep", "-f", pattern).Output()
		if err != nil {
			// pgrep returns exit code 1 when no matches found, which is normal
			continue
		}

		pids := strings.TrimSpace(string(out))
		if pids != "" {
			// Split on newlines and add to our collection
			pidList := strings.Split(pids, "\n")
			allPids = append(allPids, pidList...)
		}
	}

	if len(allPids) == 0 {
		log.Info("No running DMS shell instances found.")
		return
	}

	// Remove duplicates (in case a process matches multiple patterns)
	uniquePids := make(map[string]bool)
	for _, pid := range allPids {
		pid = strings.TrimSpace(pid)
		if pid != "" {
			uniquePids[pid] = true
		}
	}

	// Kill each unique process
	for pid := range uniquePids {
		pidInt, err := strconv.Atoi(pid)
		if err != nil {
			log.Errorf("Invalid PID %s: %v", pid, err)
			continue
		}

		proc, err := os.FindProcess(pidInt)
		if err != nil {
			log.Errorf("Error finding process %s: %v", pid, err)
			continue
		}

		if err := proc.Kill(); err != nil {
			log.Errorf("Error killing process %s: %v", pid, err)
		} else {
			log.Infof("Killed DMS shell process with PID %s", pid)
		}
	}
}

func runShellDaemon() {
	socketPath := server.GetSocketPath()

	go func() {
		if err := server.Start(); err != nil {
			log.Errorf("Server error: %v", err)
		}
	}()

	cmd := exec.Command("qs", "-c", "dms")
	cmd.Env = append(os.Environ(), "DMS_SOCKET="+socketPath)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
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

	log.Infof("DMS shell started as daemon (PID: %d)", cmd.Process.Pid)
	log.Infof("Socket: %s", socketPath)
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

	cmdArgs := append([]string{"-c", "dms", "ipc"}, args...)
	cmd := exec.Command("qs", cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("Error running IPC command: %v", err)
	}
}
