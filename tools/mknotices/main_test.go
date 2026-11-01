package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseModules(t *testing.T) {
	t.Parallel()
	list := "example.com/b\tv1.0.0\t/mod/b\n\nexample.com/a\tv2.0.0\t/mod/a\nexample.com/b\tv1.0.0\t/mod/b\n"
	got, err := parseModules(list)
	if err != nil {
		t.Fatal(err)
	}
	want := []module{{"example.com/a", "v2.0.0", "/mod/a"}, {"example.com/b", "v1.0.0", "/mod/b"}}
	if !slices.Equal(got, want) {
		t.Errorf("parseModules = %v, want %v", got, want)
	}
	if _, err := parseModules("not a module line\n"); err == nil {
		t.Error("no error for a bad line")
	}
}

func folderWith(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("text of "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLicenseFiles(t *testing.T) {
	t.Parallel()
	dir := folderWith(t, "LICENSE-SQLITE", "License.md", "COPYING", "NOTICE", "main.go", "README.md")
	if err := os.Mkdir(filepath.Join(dir, "licenses"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := licenseFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"COPYING", "LICENSE-SQLITE", "License.md", "NOTICE"}
	if !slices.Equal(got, want) {
		t.Errorf("licenseFiles = %v, want %v", got, want)
	}
}

func TestWrite(t *testing.T) {
	t.Parallel()
	goroot := folderWith(t, "LICENSE")
	mods := []module{{path: "example.com/a", version: "v1.2.3", dir: folderWith(t, "LICENSE", "LICENSE-EXTRA")}}
	var out bytes.Buffer
	if err := write(&out, goroot, mods); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Go standard library", "example.com/a v1.2.3 (LICENSE)", "example.com/a v1.2.3 (LICENSE-EXTRA)", "text of LICENSE-EXTRA"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("notices are missing %q:\n%s", want, out.String())
		}
	}
}

func TestWriteFailsWithoutALicense(t *testing.T) {
	t.Parallel()
	mods := []module{{path: "example.com/none", dir: folderWith(t, "main.go")}}
	err := write(&bytes.Buffer{}, folderWith(t, "LICENSE"), mods)
	if err == nil || !strings.Contains(err.Error(), "example.com/none") {
		t.Errorf("err = %v, want one naming the module", err)
	}
}
