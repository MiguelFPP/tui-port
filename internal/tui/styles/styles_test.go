package styles

import "testing"

func TestCatalog(t *testing.T) {
	t.Parallel()

	names := ThemeNames()
	if len(names) != len(Themes()) || len(names) == 0 {
		t.Fatalf("ThemeNames() = %v", names)
	}
	if names[0] != DefaultTheme().Name {
		t.Errorf("the first theme %q must be the default", names[0])
	}

	for _, theme := range Themes() {
		resolved, ok := ThemeByName(" " + theme.Name + " ")
		if !ok || resolved != theme {
			t.Errorf("ThemeByName(%q) = %+v, %v", theme.Name, resolved, ok)
		}
	}
}

func TestThemeByNameRejectsUnknown(t *testing.T) {
	t.Parallel()

	if _, ok := ThemeByName("nope"); ok {
		t.Error("ThemeByName must reject an unknown name")
	}
}

func TestApplyBuildsTokens(t *testing.T) {
	t.Parallel()

	theme := Themes()[1]
	Apply(theme)

	if CurrentTheme() != theme {
		t.Errorf("CurrentTheme() = %+v, want %+v", CurrentTheme(), theme)
	}
	if got := Border.GetBorderStyle().Top; got == "" {
		t.Error("Border must be framed")
	}
	if plain := CursorPlain.Render("x"); plain == "" {
		t.Error("CursorPlain must render")
	}

	Apply(DefaultTheme())
}
