package app

import (
	"context"
	"fmt"
	"os"

	"github.com/pkg/errors"
)

func Main(main func(context.Context) error) {
	if err := run(main); err != nil && errors.Cause(err) != context.Canceled {
		fmt.Fprintf(os.Stderr, "%#v\n", err)
		os.Exit(1)
	}
}

func run(main func(context.Context) error) error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	CancelOnInterrupt(ctx, cancelFunc)

	return main(ctx)
}
