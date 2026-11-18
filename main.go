package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/YusufKer/worklog/internal/config"
	"github.com/YusufKer/worklog/internal/store"
	"github.com/YusufKer/worklog/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "report" {
		if err := report(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "worklog report:", err)
			os.Exit(1)
		}
		return
	}

	dirFlag := flag.String("dir", "", "data directory (overrides $WORKLOG_DIR and the config file)")
	configFlag := flag.String("config", config.DefaultPath(), "config file")
	demo := flag.Bool("demo", false, "run against a temporary directory seeded with sample todos")
	flag.Parse()

	if err := run(*dirFlag, *configFlag, *demo); err != nil {
		fmt.Fprintln(os.Stderr, "worklog:", err)
		os.Exit(1)
	}
}

func run(dirFlag, configPath string, demo bool) error {
	cfg, err := loadConfig(configPath, demo)
	if err != nil {
		return err
	}

	dir := dataDir(dirFlag, cfg)
	if demo {
		if dir, err = os.MkdirTemp("", "worklog-demo-"); err != nil {
			return err
		}
		if err := seedDemo(dir); err != nil {
			return err
		}
		fmt.Println("demo data in", dir)
	}

	s, err := store.New(dir)
	if err != nil {
		return err
	}
	todos, skipped, err := s.LoadAll()
	if err != nil {
		return err
	}

	glamStyle := "light"
	if lipgloss.HasDarkBackground() {
		glamStyle = "dark"
	}

	m := ui.New(s, todos, ui.Options{
		GlamStyle:     glamStyle,
		Config:        cfg,
		ConfigPath:    configPath,
		Skipped:       skipped,
		DirOverridden: demo || dirFlag != "" || os.Getenv("WORKLOG_DIR") != "",
	})
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// loadConfig reads the config file. In demo mode a broken config is
// reported and ignored so -demo always starts.
func loadConfig(path string, demo bool) (config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil && demo {
		fmt.Fprintln(os.Stderr, "worklog: ignoring config:", err)
		return config.Config{}, nil
	}
	return cfg, err
}

// dataDir picks the data directory: -dir, then $WORKLOG_DIR, then the
// config file, then ~/worklog.
func dataDir(dirFlag string, cfg config.Config) string {
	for _, d := range []string{dirFlag, os.Getenv("WORKLOG_DIR"), cfg.Dir} {
		if d != "" {
			return config.ExpandHome(d)
		}
	}
	return config.ExpandHome("~/worklog")
}

func seedDemo(dir string) error {
	now := time.Now()
	day := func(offset int) string { return now.AddDate(0, 0, offset).Format(store.DateLayout) }
	// span renders a closed time entry starting `ago` before now.
	span := func(ago, dur time.Duration) string {
		start := now.Add(-ago).Truncate(time.Minute)
		return fmt.Sprintf("  - start: %s\n    end: %s\n", start.Format(time.RFC3339), start.Add(dur).Format(time.RFC3339))
	}
	samples := []struct {
		name, title, date string
		done              bool
		notes             string
		time              string
	}{
		{"fix-checkout-bug", "Fix checkout bug", day(0), false,
			"# Fix checkout bug\n\nCustomers see a double charge when they refresh on the payment step.\n\n## Repro\n1. Add item to cart\n2. Go to payment\n3. Refresh mid-request\n\n## Ideas\n- Idempotency keys on the payment intent\n- Disable the button after submit\n\n```go\nreq.Header.Set(\"Idempotency-Key\", orderID)\n```\n",
			span(3*time.Hour, 80*time.Minute)},
		{"write-sync-spec", "Write inventory sync spec", day(0), false,
			"Draft the spec for the new inventory sync.\n\n- [ ] Polling vs webhooks\n- [ ] Rate limits\n- [ ] Error handling\n", ""},
		{"deploy-api", "Deploy API v2", day(-1), true, "Deployed at 16:40. No errors in logs.",
			span(26*time.Hour, 2*time.Hour) + span(22*time.Hour, 45*time.Minute)},
		{"review-pr", "Review search PR", day(-1), false, "", ""},
		{"call-supplier", "Call supplier about pricing", day(-3), false, "Ask about volume discounts.\n", ""},
		{"plan-q1", "Plan Q1 roadmap", day(-9), true, "## Themes\n1. Reliability\n2. Onboarding\n3. Reporting\n", ""},
	}
	for i, sm := range samples {
		created := now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339)
		timeYAML := ""
		if sm.time != "" {
			timeYAML = "time:\n" + sm.time
		}
		body := fmt.Sprintf("---\nid: %s\ntitle: %s\ndate: %s\ndone: %t\ncreated: %s\n%s---\n\n%s",
			sm.name, sm.title, sm.date, sm.done, created, timeYAML, sm.notes)
		if err := os.WriteFile(filepath.Join(dir, sm.date+"-"+sm.name+".md"), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}
