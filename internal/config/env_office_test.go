package config

import "testing"

func TestOfficeEnvDefaults(t *testing.T) {
	t.Setenv("EWIKI_COLLABORA_URL", "http://collabora:9980")
	t.Setenv("EWIKI_ONLYOFFICE_JWT_SECRET", "s3cret")
	t.Setenv("EWIKI_MEDIA_VIEWER", "office.collabora")
	t.Setenv(EnvExtraTLSHosts, "Office.Example.com, bad_host, ,docs.example.com;x")
	c := &Config{Settings: map[string]map[string]any{"office.onlyoffice": {"jwt_secret": "from-config"}}}
	applyEnv(c)
	if c.Settings["office.collabora"]["url"] != "http://collabora:9980" {
		t.Fatalf("collabora url %v", c.Settings["office.collabora"])
	}
	if c.Settings["office.onlyoffice"]["jwt_secret"] != "from-config" {
		t.Fatal("environment overrode a configured value")
	}
	if c.Media.Viewer != "office.collabora" {
		t.Fatalf("viewer %q", c.Media.Viewer)
	}
	c = &Config{Media: Media{Viewer: "internal"}}
	t.Setenv("EWIKI_MEDIA_VIEWER", "office.collabora")
	applyEnv(c)
	if c.Media.Viewer != "internal" {
		t.Fatal("environment overrode the chosen viewer")
	}
	hosts := ExtraTLSHosts()
	if len(hosts) != 1 || hosts[0] != "office.example.com" {
		t.Fatalf("extra TLS hosts %v", hosts)
	}
}
