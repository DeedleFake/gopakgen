package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

type TempModule struct {
	dir string
}

func NewTempModule(ctx context.Context) (*TempModule, error) {
	dir, err := os.MkdirTemp("", "gopakgen")
	if err != nil {
		return nil, fmt.Errorf("create temporary directory: %w", err)
	}

	t := TempModule{dir: dir}
	err = t.run(ctx, "mod", "init", "tmp")
	if err != nil {
		return &t, fmt.Errorf("init tmp mod in %q: %w", dir, err)
	}

	return &t, nil
}

func (m *TempModule) Close() error {
	return os.RemoveAll(m.dir)
}

func (m *TempModule) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = m.dir
	return cmd.Run()
}

func (m *TempModule) AddModule(ctx context.Context, module string) error {
	return m.run(ctx, "get", module)
}

func (m *TempModule) ModFile() (*modfile.File, error) {
	file, err := os.ReadFile(filepath.Join(m.dir, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("open go.mod: %w", err)
	}

	mod, err := modfile.Parse("go.mod", file, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod: %w", err)
	}

	return mod, nil
}
