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
	go printASCII()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	socketPath := server.GetSocketPath()

	errChan := make(chan error, 2)

	go func() {
		if err := server.Start(false); err != nil {
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
	patterns := []string{
		"dms run",
		"qs.*dms",
		"qs.*DankMaterialShell",
		"quickshell.*dms",
		"quickshell.*DankMaterialShell",
	}

	var allPids []string
	pidChan := make(chan []string, len(patterns))

	for _, pattern := range patterns {
		go func(p string) {
			out, err := exec.Command("pgrep", "-f", p).Output()
			if err != nil {
				pidChan <- nil
				return
			}

			pids := strings.TrimSpace(string(out))
			if pids != "" {
				pidChan <- strings.Split(pids, "\n")
			} else {
				pidChan <- nil
			}
		}(pattern)
	}

	for i := 0; i < len(patterns); i++ {
		if pidList := <-pidChan; pidList != nil {
			allPids = append(allPids, pidList...)
		}
	}

	if len(allPids) == 0 {
		log.Info("No running DMS shell instances found.")
		return
	}

	currentPid := os.Getpid()
	uniquePids := make(map[string]bool)
	for _, pid := range allPids {
		pid = strings.TrimSpace(pid)
		if pid != "" {
			pidInt, err := strconv.Atoi(pid)
			if err == nil && pidInt != currentPid {
				uniquePids[pid] = true
			}
		}
	}

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
			log.Infof("Killed DMS process with PID %s", pid)
		}
	}
}

func runShellDaemon() {
	isDaemonChild := os.Getenv("DMS_DAEMON_CHILD") == "1"

	if !isDaemonChild {
		cmd := exec.Command(os.Args[0], os.Args[1:]...)
		cmd.Env = append(os.Environ(), "DMS_DAEMON_CHILD=1")

		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
		}

		if err := cmd.Start(); err != nil {
			log.Fatalf("Error starting daemon: %v", err)
		}

		log.Infof("DMS shell daemon started (PID: %d)", cmd.Process.Pid)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	socketPath := server.GetSocketPath()

	errChan := make(chan error, 2)

	go func() {
		if err := server.Start(false); err != nil {
			errChan <- fmt.Errorf("server error: %w", err)
		}
	}()

	cmd := exec.CommandContext(ctx, "qs", "-c", "dms")
	cmd.Env = append(os.Environ(), "DMS_SOCKET="+socketPath)

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

	cmdArgs := append([]string{"-c", "dms", "ipc"}, args...)
	cmd := exec.Command("qs", cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("Error running IPC command: %v", err)
	}
}
