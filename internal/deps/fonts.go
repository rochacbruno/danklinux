package deps

import (
	"os"
	"path/filepath"
	"strings"
)

type FontDetector struct {
	logChan chan<- string
}

func NewFontDetector(logChan chan<- string) *FontDetector {
	return &FontDetector{logChan: logChan}
}

func (f *FontDetector) DetectFont(fontName string) (bool, error) {
	fontPaths := []string{
		"/usr/share/fonts",
		"/usr/local/share/fonts", 
		filepath.Join(os.Getenv("HOME"), ".local/share/fonts"),
		filepath.Join(os.Getenv("HOME"), ".fonts"),
	}

	for _, basePath := range fontPaths {
		if f.searchFontInPath(basePath, fontName) {
			return true, nil
		}
	}

	return false, nil
}

func (f *FontDetector) searchFontInPath(basePath, fontName string) bool {
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		return false
	}

	found := false
	filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		
		fileName := strings.ToLower(info.Name())
		searchName := strings.ToLower(fontName)
		
		if strings.Contains(fileName, searchName) || 
		   strings.Contains(fileName, strings.ReplaceAll(searchName, "-", "")) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	
	return found
}