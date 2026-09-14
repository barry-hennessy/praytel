package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/barry-hennessy/praytel/internal/receiver"
)

var (
	ErrUsage = errors.New("usage: praytel [flags] -- command [args ...]")
)

type config struct {
	command  []string
	grpcPort int
	httpPort int
}

func parseArgs(args []string, errOut io.Writer) (config, error) {
	flags, command, ok := splitDashDash(args)
	if !ok {
		return config{}, ErrUsage
	}

	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return config{}, ErrUsage
	}

	fs := flag.NewFlagSet("praytel", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		fmt.Fprintln(errOut, ErrUsage.Error())
	}

	grpcPort := fs.Int("grpc-port", receiver.DefaultGRPCPort, "OTLP gRPC listen port")
	httpPort := fs.Int("http-port", receiver.DefaultHTTPPort, "OTLP HTTP listen port")

	if err := fs.Parse(flags); err != nil {
		return config{}, err
	}

	if fs.NArg() != 0 {
		return config{}, ErrUsage
	}

	return config{
		command:  command,
		grpcPort: *grpcPort,
		httpPort: *httpPort,
	}, nil
}

func splitDashDash(args []string) (flags, command []string, ok bool) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:], true
		}
	}

	return nil, nil, false
}
