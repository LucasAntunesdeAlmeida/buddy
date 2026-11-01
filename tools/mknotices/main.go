// Command mknotices writes the licenses of everything compiled into the given
// packages to one file, because binaries that ship BSD and MIT code must carry
// those licenses. It fails if a module has no license to copy, so a new
// dependency can't slip into a release without one.
package main

import (
	"bytes"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// extraFiles are license texts outside a module's top folder. The Go fonts
// have their own terms, in a README next to the font files.
var extraFiles = map[string][]string{
	"golang.org/x/image": {"font/gofont/ttfs/README"},
}

// module is one dependency that ends up in the binaries.
type module struct{ path, version, dir string }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mknotices:", err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("out", "THIRD_PARTY_NOTICES.txt", "file to write")
	flag.Parse()
	if flag.NArg() == 0 {
		return errors.New("name the packages that ship, like ./cmd/buddy")
	}
	mods, err := shippedModules(flag.Args())
	if err != nil {
		return err
	}
	goroot, err := goOutput("env", "GOROOT")
	if err != nil {
		return err
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	if err := write(f, strings.TrimSpace(goroot), mods); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// shippedModules lists the modules of everything the packages compile in for
// Windows, leaving out this module and tools that only run during the build.
func shippedModules(pkgs []string) ([]module, error) {
	const format = `{{with .Module}}{{if not .Main}}{{.Path}}{{"\t"}}{{.Version}}{{"\t"}}{{.Dir}}{{end}}{{end}}`
	args := append([]string{"list", "-deps", "-f", format}, pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w: %s", err, stderr.String())
	}
	return parseModules(string(b))
}

// parseModules reads the lines of go list, one per package, into one sorted
// entry per module.
func parseModules(list string) ([]module, error) {
	var mods []module
	for line := range strings.Lines(list) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected go list line: %q", line)
		}
		mods = append(mods, module{path: parts[0], version: parts[1], dir: parts[2]})
	}
	slices.SortFunc(mods, func(a, b module) int { return cmp.Compare(a.path, b.path) })
	return slices.CompactFunc(mods, func(a, b module) bool { return a.path == b.path }), nil
}

func goOutput(args ...string) (string, error) {
	b, err := exec.Command("go", args...).Output()
	if err != nil {
		return "", fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return string(b), nil
}

// licenseFiles returns the license files at the top of a module.
func licenseFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		name := strings.ToUpper(e.Name())
		for _, prefix := range []string{"LICENSE", "LICENCE", "COPYING", "NOTICE"} {
			if !e.IsDir() && strings.HasPrefix(name, prefix) {
				files = append(files, e.Name())
				break
			}
		}
	}
	return files, nil
}

func write(w io.Writer, goroot string, mods []module) error {
	fmt.Fprintln(w, "Buddy is built from these projects. Each one's license follows.")
	if err := section(w, "The Go standard library and runtime", filepath.Join(goroot, "LICENSE")); err != nil {
		return err
	}
	for _, m := range mods {
		names, err := licenseFiles(m.dir)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return fmt.Errorf("%s has no license file in %s", m.path, m.dir)
		}
		names = append(names, extraFiles[m.path]...)
		for _, name := range names {
			title := fmt.Sprintf("%s %s (%s)", m.path, m.version, name)
			if err := section(w, title, filepath.Join(m.dir, filepath.FromSlash(name))); err != nil {
				return err
			}
		}
	}
	return nil
}

func section(w io.Writer, title, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "\n%s\n%s\n\n%s\n", strings.Repeat("=", 72), title, bytes.TrimSpace(b))
	return err
}
