package styles

import (
	"testing"
)

func TestThemeCycling(t *testing.T) {
	initialTheme := AvailableThemes[0].Name
	if AvailableThemes[ActiveThemeIdx].Name != initialTheme {
		t.Fatalf("expected initial theme %s, got %s", initialTheme, AvailableThemes[ActiveThemeIdx].Name)
	}

	nextName := CycleTheme()
	if nextName != "Cyberpunk Neon" {
		t.Fatalf("expected Cyberpunk Neon, got %s", nextName)
	}

	for i := 0; i < len(AvailableThemes)-1; i++ {
		CycleTheme()
	}

	if AvailableThemes[ActiveThemeIdx].Name != initialTheme {
		t.Fatalf("expected theme to cycle back to %s, got %s", initialTheme, AvailableThemes[ActiveThemeIdx].Name)
	}
}
