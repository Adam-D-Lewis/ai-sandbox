package mountresolver

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A bare entry (no colon) mounts the host path at the same path in the
// container — the historical behavior — after placeholder expansion.
func TestResolve_BareEntry_MountsSrcToSrc(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(home, "file")
	touch(t, src)

	got := Resolve([]string{"{{HOME}}/file"}, nil, Env{Home: home}, nil)
	want := []string{src + ":" + src}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// "src:dest" remaps the path inside the container and expands both sides
// independently. Only the source must exist on the host.
func TestResolve_SrcDest_RemapsAndExpandsBothSides(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	src := filepath.Join(cwd, "sandbox.gitconfig")
	touch(t, src)

	got := Resolve(
		[]string{"{{CWD}}/sandbox.gitconfig:{{HOME}}/.gitconfig"},
		nil, Env{Home: home, CWD: cwd}, nil,
	)
	want := []string{src + ":" + filepath.Join(home, ".gitconfig")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// Existence is checked against the source only; a missing source drops the
// whole entry even when a dest is given.
func TestResolve_MissingSrc_Skipped(t *testing.T) {
	got := Resolve([]string{"/no/such/path:/dest"}, nil, Env{}, nil)
	if len(got) != 0 {
		t.Fatalf("got %#v, want empty", got)
	}
}
