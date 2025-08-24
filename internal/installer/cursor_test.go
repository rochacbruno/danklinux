package installer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/AvengeMedia/dankinstall/internal/deps"
)

func TestCursorThemeInstallation(t *testing.T) {
	logChan := make(chan string, 100)
	installer := NewArchInstaller(logChan)
	
	t.Run("skip if cursor theme already installed", func(t *testing.T) {
		dependencies := []deps.Dependency{
			{
				Name:   "bibata-cursor",
				Status: deps.StatusInstalled,
			},
		}
		
		progressChan := make(chan InstallProgressMsg, 10)
		
		err := installer.installCursorTheme(context.Background(), dependencies, "testpass", progressChan)
		assert.NoError(t, err)
		
		// Should receive a progress message indicating it's already installed
		select {
		case progress := <-progressChan:
			assert.Equal(t, PhaseCursorTheme, progress.Phase)
			assert.Contains(t, progress.Step, "already installed")
			assert.Contains(t, progress.LogOutput, "already available")
		default:
			t.Fatal("Expected progress message not received")
		}
	})
	
	t.Run("skip if cursor theme not in dependencies", func(t *testing.T) {
		dependencies := []deps.Dependency{
			{
				Name:   "other-package",
				Status: deps.StatusMissing,
			},
		}
		
		progressChan := make(chan InstallProgressMsg, 10)
		
		err := installer.installCursorTheme(context.Background(), dependencies, "testpass", progressChan)
		assert.NoError(t, err)
		
		// Should not send any progress messages
		select {
		case <-progressChan:
			t.Fatal("Should not receive progress message when cursor theme not needed")
		default:
			// Expected - no progress message
		}
	})
}

func TestCursorThemeDetection(t *testing.T) {
	// Test that cursor theme would be detected in a full dependency check
	logChan := make(chan string, 100)
	detector := deps.NewArchDetector(logChan)
	
	// Run full detection to see if cursor theme is included
	dependencies, err := detector.DetectDependencies(context.Background(), deps.WindowManagerNiri)
	assert.NoError(t, err)
	
	// Find cursor theme in dependencies
	var cursorDep *deps.Dependency
	for _, dep := range dependencies {
		if dep.Name == "bibata-cursor" {
			cursorDep = &dep
			break
		}
	}
	
	assert.NotNil(t, cursorDep, "Cursor theme should be detected as dependency")
	if cursorDep != nil {
		assert.Equal(t, "bibata-cursor", cursorDep.Name)
		assert.Equal(t, "Modern cursor theme for better visual experience", cursorDep.Description)
		assert.True(t, cursorDep.Required)
		// Status would be StatusMissing or StatusInstalled depending on system state
	}
}