package main

import (
	"fmt"
	"os"
	"os/exec"
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
