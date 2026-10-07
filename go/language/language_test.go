package language

import "testing"

func TestDetectReadsTheText(t *testing.T) {
	fr := "Synthiz transcrit et résume vos vidéos, podcasts et articles, puis retrouve la source exacte d'une idée des mois plus tard."
	en := "All-in-one protection for your identity, credit and finances. Every adult member gets coverage for eligible losses due to identity theft."
	it := "Proteggi la tua identità, il tuo credito e le tue finanze con un unico abbonamento per tutta la famiglia, ogni giorno."
	for want, text := range map[string]string{"fr": fr, "en": en, "it": it} {
		if got := Detect(text); got != want {
			t.Errorf("Detect(%q) = %q, want %q", text[:30], got, want)
		}
	}
	if got := Detect("Obsidian et le second cerveau excellent à relier les idées.", "Stocker n'est pas construire une mémoire"); got != "fr" {
		t.Errorf("several french texts read as french, got %q", got)
	}
}

func TestDetectRefusesThinMaterial(t *testing.T) {
	if got := Detect("Aura"); got != "" {
		t.Errorf("a three-letter title must not read as any language, got %q", got)
	}
	if got := Detect(); got != "" {
		t.Errorf("no material reads as no language, got %q", got)
	}
}

func TestNormalizeKeepsThePrimarySubtag(t *testing.T) {
	for in, want := range map[string]string{"en-US": "en", " FR ": "fr", "pt_BR": "pt", "français": "", "": ""} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOr(t *testing.T) {
	if got := Or("", "fr"); got != "fr" {
		t.Errorf("Or(\"\", fr) = %q", got)
	}
	if got := Or("en", "fr"); got != "en" {
		t.Errorf("Or(en, fr) = %q", got)
	}
}
