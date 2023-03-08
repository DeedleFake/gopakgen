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
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/sync/errgroup"
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

type Info struct {
	Path string `json:"-"`

	Version string
	Time    time.Time
	Origin  struct {
		VCS  string
		URL  string
		Ref  string
		Hash string
	}
}

func info(ctx context.Context, path, version string) (Info, error) {
	data, err := fetch(ctx, proxyURL(path, "@v/"+version+".info"))
	if err != nil {
		return Info{}, fmt.Errorf("fetch: %w", err)
	}

	info := Info{Path: path}
	err = json.Unmarshal(data, &info)
	if err != nil {
		return info, fmt.Errorf("parse: %w", err)
	}

	return info, nil
}

type Source struct {
	Type   string `json:"type"`
	URL    string `json:"url"`
	Tag    string `json:"tag"`
	Commit string `json:"commit"`
	Dest   string `json:"dest"`
}

func run(ctx context.Context) error {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v <module path>\n", os.Args[0])
	}
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

	infos := make([]Info, 0, len(mod.Require))
	acc := make(chan Info)
	eg, ctx := errgroup.WithContext(ctx)
	for _, r := range mod.Require {
		r := r
		eg.Go(func() error {
			path, err := module.EscapePath(r.Mod.Path)
			if err != nil {
				return fmt.Errorf("escape path %q: %w", r.Mod.Path, err)
			}

			version, err := module.EscapeVersion(r.Mod.Version)
			if err != nil {
				return fmt.Errorf("escape version %q: %w", r.Mod.Version, err)
			}

			info, err := info(ctx, path, version)
			if err != nil {
				return fmt.Errorf("info for %q: %w", r.Mod.String(), err)
			}

			select {
			case <-ctx.Done():
			case acc <- info:
			}
			return context.Cause(ctx)
		})
	}

	go func() {
		eg.Wait()
		close(acc)
	}()

	for info := range acc {
		infos = append(infos, info)
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	out := make([]Source, 0, len(infos))
	for _, info := range infos {
		out = append(out, Source{
			Type:   info.Origin.VCS,
			URL:    info.Origin.URL,
			Tag:    strings.TrimPrefix(info.Origin.Ref, "refs/tags/"),
			Commit: info.Origin.Hash,
			Dest:   filepath.Join("vendor/", filepath.FromSlash(info.Path)),
		})
	}

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
