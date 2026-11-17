package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yusuf/worklog/internal/store"
	"github.com/yusuf/worklog/internal/ui"
)

func main() {
	dirFlag := flag.String("dir", "", "data directory (default $WORKLOG_DIR or ~/worklog)")
	demo := flag.Bool("demo", false, "run against a temporary directory seeded with sample todos")
	flag.Parse()

	if err := run(*dirFlag, *demo); err != nil {
		fmt.Fprintln(os.Stderr, "worklog:", err)
		os.Exit(1)
	}
}

func run(dir string, demo bool) error {
	switch {
	case demo:
		d, err := os.MkdirTemp("", "worklog-demo-")
		if err != nil {
			return err
		}
		if err := seedDemo(d); err != nil {
			return err
		}
		fmt.Println("demo data in", d)
		dir = d
	case dir == "":
		dir = os.Getenv("WORKLOG_DIR")
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(home, "worklog")
	}

	s, err := store.New(dir)
	if err != nil {
		return err
	}
	todos, err := s.LoadAll()
	if err != nil {
		return err
	}

	glamStyle := "light"
	if lipgloss.HasDarkBackground() {
		glamStyle = "dark"
	}

	_, err = tea.NewProgram(ui.New(s, todos, glamStyle), tea.WithAltScreen()).Run()
	return err
}

func seedDemo(dir string) error {
	now := time.Now()
	day := func(offset int) string { return now.AddDate(0, 0, offset).Format(store.DateLayout) }
	samples := []struct {
		name, title, date string
		done              bool
		notes             string
	}{
		{"fix-checkout-bug", "Fix checkout bug", day(0), false,
			"# Fix checkout bug\n\nCustomers see a double charge when they refresh on the payment step.\n\n## Repro\n1. Add item to cart\n2. Go to payment\n3. Refresh mid-request\n\n## Ideas\n- Idempotency keys on the payment intent\n- Disable the button after submit\n\n```go\nreq.Header.Set(\"Idempotency-Key\", orderID)\n```\n"},
		{"write-sync-spec", "Write inventory sync spec", day(0), false,
			"Draft the spec for the new inventory sync.\n\n- [ ] Polling vs webhooks\n- [ ] Rate limits\n- [ ] Error handling\n"},
		{"deploy-api", "Deploy API v2", day(-1), true, "Deployed at 16:40. No errors in logs."},
		{"review-pr", "Review search PR", day(-1), false, ""},
		{"call-supplier", "Call supplier about pricing", day(-3), false, "Ask about volume discounts.\n"},
		{"plan-q1", "Plan Q1 roadmap", day(-9), true, "## Themes\n1. Reliability\n2. Onboarding\n3. Reporting\n"},
	}
	for i, sm := range samples {
		created := now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339)
		body := fmt.Sprintf("---\nid: %s\ntitle: %s\ndate: %s\ndone: %t\ncreated: %s\n---\n\n%s",
			sm.name, sm.title, sm.date, sm.done, created, sm.notes)
		if err := os.WriteFile(filepath.Join(dir, sm.date+"-"+sm.name+".md"), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}
