package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/YusufKer/worklog/internal/config"
	"github.com/YusufKer/worklog/internal/store"
)

// add creates a todo from the shell: worklog add [-date D] [-notes text] title...
// "-notes -" reads the notes from in (stdin). Stdin is never read otherwise,
// so a script with an open but silent stdin can't hang here.
func add(args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	date := fs.String("date", "today", `date: YYYY-MM-DD, "today", "tomorrow", "yesterday", or +N/-N days`)
	notes := fs.String("notes", "", `notes body; "-" reads it from stdin`)
	dirFlag := fs.String("dir", "", "data directory (overrides $WORKLOG_DIR and the config file)")
	configPath := fs.String("config", config.DefaultPath(), "config file")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: worklog add [-date D] [-notes text] title words...")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // usage already printed
		}
		return err
	}
	title := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if title == "" {
		return errors.New("a title is required; see worklog add -h")
	}
	body := *notes
	if body == "-" {
		b, err := io.ReadAll(in)
		if err != nil {
			return err
		}
		body = string(b)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	s, err := store.New(dataDir(*dirFlag, cfg))
	if err != nil {
		return err
	}
	t, err := s.Create(title, store.ResolveDate(*date, time.Now()))
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) != "" {
		if t, err = s.Update(t, func(x *store.Todo) { x.Notes = body }); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(out, t.Path)
	return err
}
