package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectPolkitAgent(t *testing.T) {
	cd := &ConfigDeployer{}
	
	// This test depends on the system having a polkit agent installed
	// We'll just test that the function doesn't crash and returns some path or error
	path, err := cd.detectPolkitAgent()
	
	if err != nil {
		// If no polkit agent is found, that's okay for testing
		assert.Contains(t, err.Error(), "no polkit agent found")
	} else {
		// If found, it should be a valid path
		assert.NotEmpty(t, path)
		assert.True(t, strings.Contains(path, "polkit"))
	}
}

func TestMergeNiriOutputSections(t *testing.T) {
	cd := &ConfigDeployer{}
	
	tests := []struct {
		name           string
		newConfig      string
		existingConfig string
		wantError      bool
		wantContains   []string
	}{
		{
			name: "no existing outputs",
			newConfig: `input {
    keyboard {
        xkb {
        }
    }
}
layout {
    gaps 5
}`,
			existingConfig: `input {
    keyboard {
        xkb {
        }
    }
}
layout {
    gaps 10
}`,
			wantError:    false,
			wantContains: []string{"gaps 5"}, // Should keep new config
		},
		{
			name: "merge single output",
			newConfig: `input {
    keyboard {
        xkb {
        }
    }
}
/-output "eDP-2" {
    mode "2560x1600@239.998993"
    position x=2560 y=0
}
layout {
    gaps 5
}`,
			existingConfig: `input {
    keyboard {
        xkb {
        }
    }
}
output "eDP-1" {
    mode "1920x1080@60.000000"
    position x=0 y=0
    scale 1.0
}
layout {
    gaps 10
}`,
			wantError: false,
			wantContains: []string{
				"gaps 5",                       // New config preserved
				`output "eDP-1"`,               // Existing output merged
				"1920x1080@60.000000",         // Existing output details
				"Outputs from existing configuration", // Comment added
			},
		},
		{
			name: "merge multiple outputs",
			newConfig: `input {
    keyboard {
        xkb {
        }
    }
}
/-output "eDP-2" {
    mode "2560x1600@239.998993"
    position x=2560 y=0
}
layout {
    gaps 5
}`,
			existingConfig: `input {
    keyboard {
        xkb {
        }
    }
}
output "eDP-1" {
    mode "1920x1080@60.000000"
    position x=0 y=0
    scale 1.0
}
/-output "HDMI-1" {
    mode "1920x1080@60.000000"
    position x=1920 y=0
}
layout {
    gaps 10
}`,
			wantError: false,
			wantContains: []string{
				"gaps 5",           // New config preserved
				`output "eDP-1"`,   // First existing output
				`/-output "HDMI-1"`, // Second existing output (commented)
				"1920x1080@60.000000", // Output details
			},
		},
		{
			name: "merge commented outputs",
			newConfig: `input {
    keyboard {
        xkb {
        }
    }
}
/-output "eDP-2" {
    mode "2560x1600@239.998993"
    position x=2560 y=0
}
layout {
    gaps 5
}`,
			existingConfig: `input {
    keyboard {
        xkb {
        }
    }
}
/-output "eDP-1" {
    mode "1920x1080@60.000000"
    position x=0 y=0
    scale 1.0
}
layout {
    gaps 10
}`,
			wantError: false,
			wantContains: []string{
				"gaps 5",             // New config preserved
				`/-output "eDP-1"`,   // Commented output preserved
				"1920x1080@60.000000", // Output details
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := cd.mergeNiriOutputSections(tt.newConfig, tt.existingConfig)
			
			if tt.wantError {
				assert.Error(t, err)
				return
			}
			
			require.NoError(t, err)
			
			for _, want := range tt.wantContains {
				assert.Contains(t, result, want, "merged config should contain: %s", want)
			}
			
			// Verify the example output was removed
			assert.NotContains(t, result, `/-output "eDP-2"`, "example output should be removed")
		})
	}
}

func TestConfigDeploymentFlow(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "dankinstall-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	
	// Set up test environment
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)
	
	// Test data
	logChan := make(chan string, 100)
	cd := NewConfigDeployer(logChan)
	
	t.Run("deploy ghostty config to empty directory", func(t *testing.T) {
		result, err := cd.deployGhosttyConfig(nil)
		require.NoError(t, err)
		
		assert.Equal(t, "Ghostty", result.ConfigType)
		assert.True(t, result.Deployed)
		assert.Empty(t, result.BackupPath) // No existing config, so no backup
		assert.FileExists(t, result.Path)
		
		// Verify content
		content, err := os.ReadFile(result.Path)
		require.NoError(t, err)
		assert.Contains(t, string(content), "font-family = Fira Code")
		assert.Contains(t, string(content), "window-decoration = false")
	})
	
	t.Run("deploy ghostty config with existing file", func(t *testing.T) {
		// Create existing config
		existingContent := "# Old config\nfont-size = 14\n"
		ghosttyPath := getGhosttyPath()
		err := os.MkdirAll(filepath.Dir(ghosttyPath), 0755)
		require.NoError(t, err)
		err = os.WriteFile(ghosttyPath, []byte(existingContent), 0644)
		require.NoError(t, err)
		
		result, err := cd.deployGhosttyConfig(nil)
		require.NoError(t, err)
		
		assert.Equal(t, "Ghostty", result.ConfigType)
		assert.True(t, result.Deployed)
		assert.NotEmpty(t, result.BackupPath) // Should have backup
		assert.FileExists(t, result.Path)
		assert.FileExists(t, result.BackupPath)
		
		// Verify backup content
		backupContent, err := os.ReadFile(result.BackupPath)
		require.NoError(t, err)
		assert.Equal(t, existingContent, string(backupContent))
		
		// Verify new content
		newContent, err := os.ReadFile(result.Path)
		require.NoError(t, err)
		assert.Contains(t, string(newContent), "font-family = Fira Code")
		assert.NotContains(t, string(newContent), "# Old config")
	})
}

// Helper function to get Ghostty config path for testing
func getGhosttyPath() string {
	return filepath.Join(os.Getenv("HOME"), ".config", "ghostty", "config")
}

func TestPolkitPathInjection(t *testing.T) {
	
	testConfig := `spawn-at-startup "{{POLKIT_AGENT_PATH}}"
other content`
	
	result := strings.Replace(testConfig, "{{POLKIT_AGENT_PATH}}", "/test/polkit/path", 1)
	
	assert.Contains(t, result, `spawn-at-startup "/test/polkit/path"`)
	assert.NotContains(t, result, "{{POLKIT_AGENT_PATH}}")
}

func TestNiriConfigStructure(t *testing.T) {
	// Verify the embedded Niri config has expected sections
	assert.Contains(t, NiriConfig, "cursor {")
	assert.Contains(t, NiriConfig, "xcursor-theme \"Bibata-Original-Ice\"")
	assert.Contains(t, NiriConfig, "xcursor-size 24")
	assert.Contains(t, NiriConfig, "input {")
	assert.Contains(t, NiriConfig, "layout {")
	assert.Contains(t, NiriConfig, "binds {")
	assert.Contains(t, NiriConfig, "{{POLKIT_AGENT_PATH}}")
	assert.Contains(t, NiriConfig, `spawn "ghostty"`)
}

func TestGhosttyConfigStructure(t *testing.T) {
	// Verify the embedded Ghostty config has expected settings
	assert.Contains(t, GhosttyConfig, "font-family = Fira Code")
	assert.Contains(t, GhosttyConfig, "window-decoration = false")
	assert.Contains(t, GhosttyConfig, "background-opacity = 0.90")
	assert.Contains(t, GhosttyConfig, "config-file = ./config-dankcolors")
}