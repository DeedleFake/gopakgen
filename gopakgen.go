package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
)

func run(ctx context.Context) error {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v <module path>\n", os.Args[0])
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	module := flag.Arg(0)

	tmp, err := NewTempModule(ctx)
	if err != nil {
		return fmt.Errorf("create temporary module: %w", err)
	}
	defer tmp.Close()

	err = tmp.AddModule(ctx, module)
	if err != nil {
		return fmt.Errorf("get module dependencies: %w", err)
	}

	mod, err := tmp.ModFile()
	if err != nil {
		return fmt.Errorf("load modfile: %w", err)
	}

	for _, dep := range mod.Require {
		fmt.Println(dep)
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
