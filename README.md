<div align="center">

<img src="assets/dank.svg" alt="DANK" width="400">

</div>

# Dank Linux

A comprehensive installer and management tool for DankMaterialShell, a modern desktop environment built on Quickshell for Wayland compositors.

- **dankinstall** Installs the Dank Linux suite for [niri](https://github.com/YaLTeR/niri) and/or [Hyprland](https://hypr.land)
  - Features the [DankMaterialShell](https://github.com/AvengeMedia/DankMaterialShell)
    - Which features a complete desktop experience with wallpapers, auto theming, notifications, lock screen, etc.
  - Offers up solid out of the box configurations as usable, featured starting points.
  - Can be installed if you already have niri/Hyprland configured
    - Will allow you to keep your existing config, or replace with Dank ones (existing configs always backed up though)
- **dms** Management wrapper for the Dank Linux Suite
  - Handle updates
  - Process ipc commands
  - Run dank shell

## Quickstart

```bash
curl -fsSL https://install.danklinux.com | sh
```

*Alternatively, download the latest [release](https://github.com/AvengeMedia/danklinux/releases)*

## Supported Distributions

**Note on Greeter**: dankinstall does not install a greeter.
- Start niri with `niri-session`, or Hyprland with `Hyprland`
- If you want a greeter such as gdm, sddm, tuigreet - follow your distribution's guide.

### Arch Linux & Derivatives

**Supported:** Arch, CachyOS, EndeavourOS, Manjaro

**Special Notes:**
- Uses native `pacman` for system packages
- AUR packages are built manually using `makepkg` (no AUR helper dependency)
- **Recommendations**
  - Use NetworkManager to manage networking
  - If using archinstall, you can choose `minimal` for profile, and `NetworkManager` under networking.

**Package Sources:**
| Package | Source | Notes |
|---------|---------|-------|
| System packages (git, jq, etc.) | Official repos | Via `pacman` |
| quickshell | AUR | Built from source |
| matugen | AUR (`matugen-bin`) | Pre-compiled binary |
| dgop | AUR | Built from source |
| niri | Official repos (`niri`) | Latest niri |
| hyprland | Official repos | Available in Extra repository |
| DankMaterialShell | Manual | Git clone to `~/.config/quickshell/dms` |

### Fedora & Derivatives

**Supported:** Fedora, Nobara, Asahi

**Special Notes:**
- Requires `dnf-plugins-core` for COPR repository support
- Automatically enables required COPR repositories
- All COPR repos are enabled with automatic acceptance
- **Editions** dankinstall is tested on "Workstation Edition", but probably works fine on any fedora flavor. Report issues if anything doesn't work.
- [Fedora Asahi Remix](https://asahilinux.org/fedora/) hasn't been tested, but presumably it should work fine as all of the dependencies should provide arm64 variants.

**Package Sources:**
| Package | Source | Notes |
|---------|---------|-------|
| System packages | Official repos | Via `dnf` |
| quickshell | COPR | `errornointernet/quickshell` |
| matugen | COPR | `solopasha/hyprland` |
| dgop | Manual | Built from source with Go |
| cliphist | COPR | `alternateved/cliphist` |
| ghostty | COPR | `alternateved/ghostty` |
| hyprland | COPR | `solopasha/hyprland` |
| niri | COPR | `yalter/niri` |
| DankMaterialShell | Manual | Git clone to `~/.config/quickshell/dms` |

### Ubuntu

**Supported:** Ubuntu 25.04+

**Special Notes:**
- Requires PPA support via `software-properties-common`
- Go installed from PPA for building manual packages
- Most packages require manual building due to limited repository availability
  - This means the install can be quite slow, as many need to be compiled from source.
  - niri is packages as a `.deb` so it can be managed via `apt`
- Automatic PPA repository addition and package list updates

**Package Sources:**
| Package | Source | Notes |
|---------|---------|-------|
| System packages | Official repos | Via `apt` |
| quickshell | Manual | Built from source with cmake |
| matugen | Manual | Built from source with Go |
| dgop | Manual | Built from source with Go |
| hyprland | PPA | `ppa:cppiber/hyprland` |
| hyprpicker | PPA | `ppa:cppiber/hyprland` |
| niri | Manual | Built from source with Rust |
| Go compiler | PPA | `ppa:longsleep/golang-backports` |
| DankMaterialShell | Manual | Git clone to `~/.config/quickshell/dms` |

### NixOS (Not Currently Support)

**NixOS is not supported atm due to the flake needing some adjustments for defaults/Hyprland, it will be re-enabled in the future.**
**You can install the [dms](https://github.com/AvengeMedia/DankMaterialShell) flake manually still and get most of what you need for this setup**

~~**Supported:** NixOS~~

**Special Notes:**
- Window managers (hyprland/niri) should be managed through `configuration.nix`, not the installer
  - This means we require hyprland or niri to be installed first.
- Uses `nix profile` for user-level package installation
- All packages sourced from nixpkgs or flakes
- DMS installed as a flake package (not `git clone` like other distributions)

Requires the following in `configuration.nix`
```
{
  nix.settings.experimental-features = ["nix-command flakes"];
}
```

**Package Sources:**
| Package | Source | Notes |
|---------|---------|-------|
| All system packages | nixpkgs | Via `nix profile install nixpkgs#package` |
| quickshell | Flake | Built from source |
| matugen | Flake | Built from source |
| dgop | Flake | Built from source |
| DankMaterialShell | Flake | Installed via nix profile |
| hyprland | System config | `programs.hyprland.enable = true` in `configuration.nix` |
| niri | System config | Add to `environment.systemPackages` in `configuration.nix` |

## Manual Package Building

The installer handles manual package building for packages not available in repositories:

### quickshell (Ubuntu, NixOS)
- Built from source using cmake
- Requires Qt6 development libraries
- Automatically handles build dependencies

### matugen (Ubuntu, NixOS, Fedora)
- Built from Go source
- Requires Go 1.19+
- Installed to `/usr/local/bin`

### dgop (All distros)
- Built from Go source
- Simple dependency-free build
- Installed to `/usr/local/bin`

### niri (Ubuntu)
- Built from Rust source
- Requires cargo and rust toolchain
- Complex build with multiple dependencies

## Commands

### dankinstall
Main installer with interactive TUI for initial setup

### dms
Management interface for DankMaterialShell:
- `dms` - Interactive management TUI
- `dms run` - Start interactive shell
- `dms run -d` - Start shell as daemon
- `dms restart` - Restart running DMS shell
- `dms kill` - Kill running DMS shell processes
- `dms ipc <command>` - Send IPC commands to running shell