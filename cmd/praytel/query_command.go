package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/barry-hennessy/praytel/internal/query"
)

func runQueryCommand(args []string, stdout io.Writer) error {
	if len(args) != 2 {
		return errors.New("usage: praytel query <check|fmt|explain> QUERY")
	}
	q, err := query.Check(args[1])
	if err != nil {
		return err
	}
	switch args[0] {
	case "check":
		return nil
	case "fmt":
		_, err = fmt.Fprintln(stdout, query.Format(q))
		return err
	case "explain":
		_, err = io.WriteString(stdout, query.Explain(q))
		return err
	default:
		return fmt.Errorf("unknown query command %q", args[0])
	}
}

func querySource(cfg config) (string, error) {
	if cfg.queryFile == "" {
		return cfg.query, nil
	}
	b, err := os.ReadFile(cfg.queryFile)
	if err != nil {
		return "", fmt.Errorf("read query file: %w", err)
	}
	return string(b), nil
}
