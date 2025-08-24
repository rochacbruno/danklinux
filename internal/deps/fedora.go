package deps

import "context"

type FedoraDetector struct {
	*BaseDetector
}

func NewFedoraDetector(logChan chan<- string) *FedoraDetector {
	return &FedoraDetector{
		BaseDetector: NewBaseDetector(logChan),
	}
}

func (f *FedoraDetector) DetectDependencies(ctx context.Context, wm WindowManager) ([]Dependency, error) {
	// For now, just use base detection
	// TODO: Add Fedora-specific package detection (dnf, rpm, etc.)
	return f.BaseDetector.DetectDependencies(ctx, wm)
}