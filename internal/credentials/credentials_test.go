package credentials

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSaveLoadDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ngp", "credentials.json")
	if c, err := Load(path); c != nil || err != nil {
		t.Fatalf("missing file: %v, %v", c, err)
	}
	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	in := &Credentials{Server: "https://s", ProxyURL: "https://p", Token: "ngp_u_x", ExpiresAt: exp}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(path)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("file mode %o, want 0600", fi.Mode().Perm())
		}
		di, _ := os.Stat(filepath.Dir(path))
		if di.Mode().Perm() != 0o700 {
			t.Errorf("dir mode %o, want 0700", di.Mode().Perm())
		}
	}
	out, err := Load(path)
	if err != nil || *out != *in {
		t.Fatalf("Load = %+v, %v", out, err)
	}
	if !out.Valid(time.Now()) || out.Valid(exp) {
		t.Error("Valid")
	}
	if err := Delete(path); err != nil {
		t.Fatal(err)
	}
	if err := Delete(path); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("NGP_CONFIG_DIR", "/x/ngp")
	if p, _ := DefaultPath(); p != filepath.Join("/x/ngp", "credentials.json") {
		t.Errorf("NGP_CONFIG_DIR: %s", p)
	}
	t.Setenv("NGP_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if p, _ := DefaultPath(); p != filepath.Join("/xdg", "ngp", "credentials.json") {
		t.Errorf("XDG_CONFIG_HOME: %s", p)
	}
}
