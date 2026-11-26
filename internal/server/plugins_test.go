package server_test

import (
	"testing"
)

func TestPluginsLiteImage(t *testing.T) {
	t.Setenv("EWIKI_IMAGE", "lite")
	t.Setenv("EWIKI_DATA", t.TempDir())
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var st struct {
		Mode             string   `json:"mode"`
		CanBuild         bool     `json:"can_build"`
		BuildUnavailable string   `json:"build_unavailable"`
		Removable        []string `json:"removable"`
	}
	admin.must("GET", "/api/v1/admin/plugins", nil, &st, 200)
	if st.Mode != "lite" || st.CanBuild || st.BuildUnavailable != "lite" || len(st.Removable) != 0 {
		t.Fatalf("lite status: %+v", st)
	}
	if code := admin.do("POST", "/api/v1/admin/plugins/install", map[string]any{"source": "github.com/x/y"}, nil); code != 400 {
		t.Fatalf("install in the lite image: %d", code)
	}
	if code := admin.do("POST", "/api/v1/admin/plugins/build", nil, nil); code != 400 {
		t.Fatalf("build in the lite image: %d", code)
	}
	if code := admin.do("DELETE", "/api/v1/admin/plugins/core.html", nil, nil); code != 400 && code != 404 {
		t.Fatalf("remove a bundled plugin: %d", code)
	}
	var opt struct {
		Auto bool `json:"auto_restart_on_drop"`
	}
	admin.must("PUT", "/api/v1/admin/plugins/options", map[string]any{"auto_restart_on_drop": true}, &opt, 200)
	if !opt.Auto || !e.app.Config.Get().Plugins.AutoRestartOnDrop {
		t.Fatal("option not saved")
	}
}
