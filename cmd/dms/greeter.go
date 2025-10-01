package main

import (
	"fmt"

	"github.com/AvengeMedia/dankinstall/internal/greeter"
)

func installGreeter() error {
	fmt.Println("=== DMS Greeter Installation ===")

	logFunc := func(msg string) {
		fmt.Println(msg)
	}

	// Step 1: Ensure greetd is installed
	if err := greeter.EnsureGreetdInstalled(logFunc, ""); err != nil {
		return err
	}

	// Step 2: Detect DMS path
	fmt.Println("\nDetecting DMS installation...")
	dmsPath, err := greeter.DetectDMSPath()
	if err != nil {
		return err
	}
	fmt.Printf("✓ Found DMS at: %s\n", dmsPath)

	// Step 3: Detect compositors
	fmt.Println("\nDetecting installed compositors...")
	compositors := greeter.DetectCompositors()
	if len(compositors) == 0 {
		return fmt.Errorf("no supported compositors found (niri or Hyprland required)")
	}

	var selectedCompositor string
	if len(compositors) == 1 {
		selectedCompositor = compositors[0]
		fmt.Printf("✓ Found compositor: %s\n", selectedCompositor)
	} else {
		var err error
		selectedCompositor, err = greeter.PromptCompositorChoice(compositors)
		if err != nil {
			return err
		}
		fmt.Printf("✓ Selected compositor: %s\n", selectedCompositor)
	}

	// Step 4: Copy greeter files
	fmt.Println("\nCopying greeter files...")
	if err := greeter.CopyGreeterFiles(dmsPath, selectedCompositor, logFunc, ""); err != nil {
		return err
	}

	// Step 5: Configure greetd
	fmt.Println("\nConfiguring greetd...")
	if err := greeter.ConfigureGreetd(logFunc, ""); err != nil {
		return err
	}

	// Step 6: Sync DMS configs
	fmt.Println("\nSynchronizing DMS configurations...")
	if err := greeter.SyncDMSConfigs(logFunc, ""); err != nil {
		return err
	}

	fmt.Println("\n=== Installation Complete ===")
	fmt.Println("\nTo test the greeter, run:")
	fmt.Println("  sudo systemctl start greetd")
	fmt.Println("\nTo enable on boot, run:")
	fmt.Println("  sudo systemctl enable --now greetd")

	return nil
}
