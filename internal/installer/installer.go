package installer

import (
	"context"
	"fmt"

	"github.com/AvengeMedia/dankinstall/internal/pkgmanager"
)

type WindowManager int

const (
	Hyprland WindowManager = iota
	Niri
)

type Installer struct {
	distribution string
	pkgManager   pkgmanager.PackageManager
	logChan      chan<- string
}

func NewInstaller(distribution string, logChan chan<- string) (*Installer, error) {
	pkgMgr, err := pkgmanager.NewPackageManager(distribution, logChan)
	if err != nil {
		return nil, err
	}

	return &Installer{
		distribution: distribution,
		pkgManager:   pkgMgr,
		logChan:      logChan,
	}, nil
}

func (i *Installer) InstallWindowManager(ctx context.Context, wm WindowManager, progressFunc pkgmanager.ProgressFunc) error {
	packages, err := i.getPackagesForWM(wm)
	if err != nil {
		return err
	}

	i.logChan <- fmt.Sprintf("Installing %s on %s", i.wmName(wm), i.distribution)
	return i.pkgManager.InstallPackages(ctx, packages, progressFunc)
}

func (i *Installer) getPackagesForWM(wm WindowManager) ([]string, error) {
	switch wm {
	case Hyprland:
		return i.getHyprlandPackages()
	case Niri:
		return i.getNiriPackages()
	default:
		return nil, fmt.Errorf("unsupported window manager")
	}
}

func (i *Installer) getHyprlandPackages() ([]string, error) {
	switch i.distribution {
	case "arch":
		return []string{"hyprland", "waybar", "wofi", "kitty"}, nil
	case "fedora":
		return []string{"hyprland", "waybar", "wofi", "kitty"}, nil
	default:
		return nil, fmt.Errorf("hyprland not supported on %s", i.distribution)
	}
}

func (i *Installer) getNiriPackages() ([]string, error) {
	switch i.distribution {
	case "arch":
		return []string{"niri", "waybar", "wofi", "kitty"}, nil
	case "fedora":
		return []string{"niri", "waybar", "wofi", "kitty"}, nil
	default:
		return nil, fmt.Errorf("niri not supported on %s", i.distribution)
	}
}

func (i *Installer) wmName(wm WindowManager) string {
	switch wm {
	case Hyprland:
		return "Hyprland"
	case Niri:
		return "Niri"
	default:
		return "Unknown"
	}
}