package main

import (
	"context"

	"golang.org/x/sync/errgroup"
)

func asyncMap[R, T any](ctx context.Context, s []T, f func(T) (R, error)) ([]R, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	r := make([]R, 0, len(s))
	c := make(chan R)
	eg, ctx := errgroup.WithContext(ctx)
	for _, v := range s {
		v := v
		eg.Go(func() error {
			r, err := f(v)
			if err != nil {
				return err
			}
			select {
			case <-ctx.Done():
			case c <- r:
			}
			return context.Cause(ctx)
		})
	}

	go func() {
		eg.Wait()
		close(c)
	}()

	for v := range c {
		r = append(r, v)
	}

	return r, eg.Wait()
}
