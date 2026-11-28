package kernel

import (
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
)

// reads counts how often the manifest of the test plugins is read.
var reads atomic.Int64

type countingPlugin struct{}

func (*countingPlugin) Manifest() coreplugin.Manifest {
	reads.Add(1)
	return coreplugin.Manifest{ID: "test.counting", Name: "Counting", Version: "1.0.0", Kinds: []string{coreplugin.KindTemplate}}
}

type extendingPlugin struct{}

func (*extendingPlugin) Manifest() coreplugin.Manifest {
	reads.Add(1)
	return coreplugin.Manifest{ID: "test.extending", Name: "Extending", Version: "1.0.0", Kinds: []string{coreplugin.KindTemplate}, Extends: []string{"test.counting"}}
}

func init() {
	coreplugin.Register(&countingPlugin{})
	coreplugin.Register(&extendingPlugin{})
}

// The kernel reads every manifest once. Reading it again (JSON) for every
// plugin and request made /site take 25 ms and more; the lists and lookups
// of a request must not read a manifest at all.
func TestManifestsReadOnce(t *testing.T) {
	st, err := config.Open(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	k := NewSite(st, slog.New(slog.NewTextHandler(io.Discard, nil)), "main")
	before := reads.Load()
	for range 10 {
		list := k.List()
		if len(list) != 2 || list[0].ID != "test.counting" || len(list[0].Dependents) != 1 {
			t.Fatalf("list: %+v", list)
		}
		_ = k.IsTemplate("test.extending")
		if m, ok := k.Manifest("test.extending"); !ok || m.Extends[0] != "test.counting" {
			t.Fatalf("manifest: %+v", m)
		}
	}
	if n := reads.Load() - before; n != 0 {
		t.Fatalf("the manifests were read %d times after the kernel was made", n)
	}
}
