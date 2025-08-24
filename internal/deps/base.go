package deps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type BaseDetector struct {
	logChan      chan<- string
	fontDetector *FontDetector
}

func NewBaseDetector(logChan chan<- string) *BaseDetector {
	return &BaseDetector{
		logChan:      logChan,
		fontDetector: NewFontDetector(logChan),
	}
}

// Common helper methods that all detectors can use
func (b *BaseDetector) commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

// Base implementations that can be overridden
func (b *BaseDetector) detectMatugen() Dependency {
	status := StatusMissing
	if b.commandExists("matugen") {
		status = StatusInstalled
	}
	
	return Dependency{
		Name:        "matugen",
		Status:      status,
		Description: "Material Design color generation tool",
		Required:    true,
	}
}

func (b *BaseDetector) detectDgop() Dependency {
	status := StatusMissing
	if b.commandExists("dgop") {
		status = StatusInstalled
	}
	
	return Dependency{
		Name:        "dgop",
		Status:      status,
		Description: "Desktop portal management tool",
		Required:    true,
	}
}

func (b *BaseDetector) detectDMS() Dependency {
	dmsPath := filepath.Join(os.Getenv("HOME"), ".config/quickshell/dms")
	
	status := StatusMissing
	if _, err := os.Stat(dmsPath); err == nil {
		status = StatusInstalled
	}
	
	return Dependency{
		Name:        "dms",
		Status:      status,
		Description: "Desktop Management System configuration",
		Required:    true,
	}
}

func (b *BaseDetector) detectClipboardTools() []Dependency {
	var deps []Dependency
	
	cliphist := StatusMissing
	if b.commandExists("cliphist") {
		cliphist = StatusInstalled
	}
	
	wlClipboard := StatusMissing  
	if b.commandExists("wl-copy") && b.commandExists("wl-paste") {
		wlClipboard = StatusInstalled
	}
	
	deps = append(deps,
		Dependency{
			Name:        "cliphist",
			Status:      cliphist,
			Description: "Wayland clipboard manager",
			Required:    true,
		},
		Dependency{
			Name:        "wl-clipboard",
			Status:      wlClipboard,
			Description: "Wayland clipboard utilities",
			Required:    true,
		},
	)
	
	return deps
}

func (b *BaseDetector) detectTerminal() Dependency {
	status := StatusMissing
	if b.commandExists("ghostty") {
		status = StatusInstalled
	}
	
	return Dependency{
		Name:        "ghostty",
		Status:      status,
		Description: "Fast, feature-rich terminal emulator",
		Required:    true,
	}
}

func (b *BaseDetector) detectCursorTheme() Dependency {
	status := StatusMissing
	if _, err := os.Stat("/usr/share/icons/Bibata-Original-Ice"); err == nil {
		status = StatusInstalled
	}
	
	return Dependency{
		Name:        "bibata-cursor",
		Status:      status,
		Description: "Modern cursor theme for better visual experience",
		Required:    true,
	}
}

func (b *BaseDetector) detectFonts() []Dependency {
	requiredFonts := []string{
		"material-symbols",
		"inter",
		"firacode",
	}
	
	var deps []Dependency
	
	for _, font := range requiredFonts {
		found, _ := b.fontDetector.DetectFont(font)
		status := StatusMissing
		if found {
			status = StatusInstalled
		}
		
		deps = append(deps, Dependency{
			Name:        "font-" + font,
			Status:      status,
			Description: strings.Title(font) + " font family",
			Required:    true,
		})
	}
	
	return deps
}

// Default implementation - can be overridden by distros
func (b *BaseDetector) DetectDependencies(ctx context.Context, wm WindowManager) ([]Dependency, error) {
	var deps []Dependency
	
	// Add base dependencies that are common across distros
	deps = append(deps, b.detectMatugen())
	deps = append(deps, b.detectDgop())
	deps = append(deps, b.detectDMS())
	deps = append(deps, b.detectFonts()...)
	deps = append(deps, b.detectClipboardTools()...)
	
	return deps, nil
}