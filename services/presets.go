package services

import "truedeploy/internal/presets"

// Presets lists the framework presets (Dockerfiles generated per framework).
func (s *ProjectService) Presets() []presets.Preset { return presets.All() }

// PreviewPreset returns the files a preset would generate with these
// options, so they can be reviewed or copied into the repository.
func (s *ProjectService) PreviewPreset(id string, opts map[string]string, port int) (map[string]string, error) {
	return presets.Render(id, opts, port)
}
