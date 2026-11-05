package main

import "testing"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadConfig(t *testing.T) {
	c, err := loadConfig(env(map[string]string{
		"BOTGUARD_UPSTREAM":        "http://app:3000",
		"BOTGUARD_PROTECT":         "/search, /contact",
		"BOTGUARD_TRUSTED_PROXIES": "10.0.0.0/8",
		"BOTGUARD_OBSERVE":         "true",
		"BOTGUARD_PER_MINUTE":      "12",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.guard.Audience != "app:3000" || len(c.protect) != 2 || len(c.guard.TrustedProxies) != 1 || c.guard.InsecureCookie {
		t.Fatalf("config: %+v", c)
	}
	p, _ := c.guard.Settings.Policy(t.Context())
	if p.Enforce || p.PerMinute != 12 {
		t.Fatalf("policy: %+v", p)
	}
	if !matchesAny("/search/x", c.protect) || matchesAny("/about", c.protect) {
		t.Fatal("protect prefixes")
	}
}

func TestLoadConfigRejects(t *testing.T) {
	for name, e := range map[string]map[string]string{
		"no upstream":      {},
		"relative":         {"BOTGUARD_UPSTREAM": "/app"},
		"bad proxy":        {"BOTGUARD_UPSTREAM": "http://a", "BOTGUARD_TRUSTED_PROXIES": "nope"},
		"bad observe flag": {"BOTGUARD_UPSTREAM": "http://a", "BOTGUARD_OBSERVE": "maybe"},
	} {
		if _, err := loadConfig(env(e)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
