package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/YusufKer/worklog/internal/config"
	"github.com/YusufKer/worklog/internal/store"
)

// report prints a week's time report: worklog report [-week W] [-csv].
func report(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	week := fs.String("week", "this", `week to report: "this", "last", -N weeks ago, YYYY-Www, or a date in the week`)
	csv := fs.Bool("csv", false, "print CSV in decimal hours instead of a Markdown table")
	dirFlag := fs.String("dir", "", "data directory (overrides $WORKLOG_DIR and the config file)")
	configPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: worklog report [-week this|last|-N|YYYY-Www|YYYY-MM-DD] [-csv]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q; see worklog report -h", fs.Arg(0))
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	now := time.Now()
	start, err := store.ParseWeek(*week, now)
	if err != nil {
		return err
	}
	s, err := store.New(dataDir(*dirFlag, cfg))
	if err != nil {
		return err
	}
	todos, skipped, err := s.LoadAll()
	if err != nil {
		return err
	}
	for _, b := range skipped {
		fmt.Fprintln(os.Stderr, "worklog report: skipped", b)
	}

	w := store.BuildWeek(todos, start, now)
	if *csv {
		_, err = io.WriteString(out, w.CSV())
	} else {
		_, err = io.WriteString(out, w.Markdown())
	}
	return err
}
