package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"golang.org/x/exp/slices"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/tools/go/vcs"
)

func proxyURL(path, endpoint string) string {
	// TODO: Respect $GOPROXY?
	return fmt.Sprintf("https://proxy.golang.org/%v/%v", path, endpoint)
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer rsp.Body.Close()

	data, err := io.ReadAll(rsp.Body)
	if err != nil {
		return data, fmt.Errorf("read response: %w", err)
	}

	return data, nil
}

func latest(ctx context.Context, module string) (string, error) {
	raw, err := fetch(ctx, proxyURL(module, "@latest"))
	if err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}

	var data struct{ Version string }
	err = json.Unmarshal(raw, &data)
	if err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return data.Version, nil
}

func mod(ctx context.Context, path, version string) (*modfile.File, error) {
	data, err := fetch(ctx, proxyURL(path, "@v/"+version+".mod"))
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	file, err := modfile.Parse(path+"@"+version+".mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	return file, nil
}

type Source struct {
	Type                string `json:"type"`
	URL                 string `json:"url"`
	Tag                 string `json:"tag,omitempty"`
	Commit              string `json:"commit,omitempty"`
	Dest                string `json:"dest"`
	DisableFsckObjects  bool   `json:"disable-fsckobjects,omitempty"`
	DisableShallowClone bool   `json:"disable-shallow-clone,omitempty"`
	DisableSubmodules   bool   `json:"disable-submodules,omitempty"`
}

func source(path, version string) (Source, error) {
	rr, err := vcs.RepoRootForImportPath(path, false)
	if err != nil {
		return Source{}, fmt.Errorf("get repo root: %w", err)
	}

	tag, commit := version, ""
	if module.IsPseudoVersion(version) {
		rev, err := module.PseudoVersionRev(version)
		if err != nil {
			return Source{}, fmt.Errorf("get pseudo-version revision for %q: %w", path+"@"+version, err)
		}
		tag, commit = "", rev
	}

	tag, incompatible := strings.CutSuffix(tag, "+incompatible")

	major := "/" + semver.Major(version)
	if incompatible || (major == "/v0") || (major == "/v1") {
		major = ""
	}
	if tag != "" {
		subdir := strings.TrimPrefix(path, rr.Root)
		subdir = strings.TrimSuffix(subdir, major)
		subdir = strings.TrimPrefix(subdir, "/")
		if subdir != "" {
			tag = subdir + "/" + tag
		}
	}

	return Source{
		Type:   rr.VCS.Cmd,
		URL:    rr.Repo,
		Tag:    tag,
		Commit: commit,
		Dest:   filepath.Join("vendor", rr.Root, major),
	}, nil
}

func run(ctx context.Context) error {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v [options] <module path>\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
	}
	disableFsckObjects := flag.Bool("disable-fsckobjects", false, "disable cloning of fsck objects from dependencies")
	disableShallowClone := flag.Bool("disable-shallow-clone", false, "disable shallow clones of dependencies")
	disableSubmodules := flag.Bool("disable-submodules", false, "disable submodules of dependencies")
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	base := flag.Arg(0)

	path, version, ok := strings.Cut(base, "@")
	if !ok || (version == "latest") {
		var err error
		version, err = latest(ctx, path)
		if err != nil {
			return fmt.Errorf("get latest version of module: %w", err)
		}
	}

	mod, err := mod(ctx, path, version)
	if err != nil {
		return fmt.Errorf("get modfile: %w", err)
	}

	out, err := asyncMap(ctx, mod.Require, func(req *modfile.Require) (Source, error) {
		s, err := source(req.Mod.Path, req.Mod.Version)
		if err != nil {
			return s, fmt.Errorf("generate source for %q: %w", req.Mod.String(), err)
		}
		switch s.Type {
		case "git":
			s.DisableFsckObjects = *disableFsckObjects
			s.DisableShallowClone = *disableShallowClone
			s.DisableSubmodules = *disableSubmodules
		}
		return s, nil
	})
	if err != nil {
		return err
	}
	slices.SortFunc(out, func(s1, s2 Source) bool { return s1.Dest < s2.Dest })

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	err = enc.Encode(out)
	if err != nil {
		return fmt.Errorf("encode output: %w", err)
	}

	return nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	err := run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
