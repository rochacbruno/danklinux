package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func runShellInteractive() {
	printASCII()
	cmd := exec.Command("qs", "-c", "dms")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("Error starting quickshell: %v\n", err)
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
		fmt.Println("No running DMS shell instances found.")
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
			fmt.Printf("Invalid PID %s: %v\n", pid, err)
			continue
		}

		proc, err := os.FindProcess(pidInt)
		if err != nil {
			fmt.Printf("Error finding process %s: %v\n", pid, err)
			continue
		}

		if err := proc.Kill(); err != nil {
			fmt.Printf("Error killing process %s: %v\n", pid, err)
		} else {
			fmt.Printf("Killed DMS shell process with PID %s\n", pid)
		}
	}
}

func runShellDaemon() {
	cmd := exec.Command("qs", "-c", "dms")

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	devNull, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		fmt.Printf("Error opening /dev/null: %v\n", err)
		os.Exit(1)
	}
	defer devNull.Close()

	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull

	if err := cmd.Start(); err != nil {
		fmt.Printf("Error starting daemon: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("DMS shell started as daemon (PID: %d)\n", cmd.Process.Pid)
}

func runShellIPCCommand(args []string) {
	if len(args) == 0 {
		fmt.Println("Error: IPC command requires arguments")
		fmt.Println("Usage: dms shell ipc <command> [args...]")
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
		fmt.Printf("Error running IPC command: %v\n", err)
		os.Exit(1)
	}
}
