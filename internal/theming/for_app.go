package theming

import (
	"github.com/framegrace/texelation/config"
	"github.com/framegrace/texelui/theme"
)

// ForApp returns the base theme merged with any per-app overrides.
func ForApp(app string) theme.Config {
	base := theme.Get()
	overrides := overridesForApp(app)
	if len(overrides) == 0 {
		return base
	}
	return theme.WithOverrides(base, overrides)
}

func overridesForApp(app string) theme.Config {
	if app == "" {
		return nil
	}
	cfg := config.App(app)
	if cfg == nil {
		return nil
	}
	return theme.ParseOverrides(cfg["theme_overrides"])
}
