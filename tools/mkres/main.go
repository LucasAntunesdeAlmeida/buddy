// Command mkres makes the Windows resources for buddy.exe: Buddy's icon,
// drawn by the same code that draws Buddy on screen, version information,
// and a manifest. It writes a .syso file that go build picks up.
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/ui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mkres:", err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("out", "rsrc_windows_amd64.syso", "file to write")
	ver := flag.String("version", "0.0.0", "version, like 1.2.3")
	flag.Parse()

	img, err := ui.IconImage(256)
	if err != nil {
		return err
	}
	icon, err := winres.NewIconFromResizedImage(img, nil)
	if err != nil {
		return fmt.Errorf("make icon: %w", err)
	}
	var rs winres.ResourceSet
	if err := rs.SetIcon(winres.Name("APPICON"), icon); err != nil {
		return fmt.Errorf("add icon: %w", err)
	}

	nums := versionNumbers(*ver)
	vi := version.Info{FileVersion: nums, ProductVersion: nums}
	for k, v := range map[string]string{
		version.ProductName:     "Buddy",
		version.FileDescription: "Buddy, a desktop companion that learns",
		version.ProductVersion:  *ver,
		version.FileVersion:     *ver,
	} {
		if err := vi.Set(version.LangDefault, k, v); err != nil {
			return fmt.Errorf("set %s: %w", k, err)
		}
	}
	rs.SetVersionInfo(vi)
	rs.SetManifest(winres.AppManifest{DPIAwareness: winres.DPIPerMonitorV2})

	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	if err := rs.WriteObject(f, winres.ArchAMD64); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", *out, err)
	}
	return f.Close()
}

// versionNumbers turns "v1.2.3-4-gabc" into {1, 2, 3, 0}; anything that
// isn't a number counts as 0.
func versionNumbers(v string) [4]uint16 {
	var n [4]uint16
	v = strings.TrimPrefix(v, "v")
	v, _, _ = strings.Cut(v, "-")
	for i, part := range strings.SplitN(v, ".", 4) {
		if x, err := strconv.ParseUint(part, 10, 16); err == nil {
			n[i] = uint16(x)
		}
	}
	return n
}
