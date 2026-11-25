package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"sync"
	"time"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	once := flag.Bool("once", false, "run a single check and exit")
	list := flag.Bool("list", false, "print every currently open job that matches the filters, then exit (no notifications, state untouched)")
	testTelegram := flag.Bool("test-telegram", false, "send a test Telegram message and exit")
	logFile := flag.String("logfile", "", "append logs to this file instead of the console (for scheduled runs)")
	flag.Parse()

	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	notifier := NewNotifier(cfg.Telegram)
	filter := NewFilter(cfg.Filters)

	if *testTelegram {
		if !notifier.Enabled() {
			log.Fatal("telegram is not configured: set bot_token and chat_id (or TELEGRAM_BOT_TOKEN / TELEGRAM_CHAT_ID)")
		}
		if err := notifier.SendText(ctx, "✅ Job tracker connected. You'll get new job alerts here."); err != nil {
			log.Fatal(err)
		}
		log.Println("test message sent")
		return
	}

	if *list {
		jobs := fetchAll(ctx, cfg, filter, nil)
		for _, j := range jobs {
			log.Printf("%-12s | %s | %s | %s\n      %s", j.Company, j.Title, truncate(j.Location, 80), yearsLabel(j), j.URL)
		}
		log.Printf("%d matching jobs open right now", len(jobs))
		return
	}

	store, err := LoadStore(cfg.StateFile)
	if err != nil {
		log.Fatalf("load state: %v", err)
	}
	if !notifier.Enabled() {
		log.Println("telegram not configured: new jobs will only be printed to this console")
	}

	for {
		start := time.Now()
		fresh := fetchAll(ctx, cfg, filter, store)
		if err := store.Save(); err != nil {
			log.Printf("save state: %v", err)
		}
		notifier.Notify(ctx, fresh)
		log.Printf("check done in %s: %d new matching jobs", time.Since(start).Round(time.Second), len(fresh))

		if *once {
			return
		}
		select {
		case <-ctx.Done():
			log.Println("stopping")
			return
		case <-time.After(cfg.PollInterval()):
		}
	}
}

// fetchAll polls every company concurrently and returns jobs that match the
// filters. With a store, only never-seen jobs are returned, and a company's
// first successful fetch is recorded silently to avoid a flood of old jobs.
func fetchAll(ctx context.Context, cfg *Config, filter *Filter, store *Store) []Job {
	var (
		mu     sync.Mutex
		result []Job
		wg     sync.WaitGroup
		sem    = make(chan struct{}, cfg.Concurrency)
	)
	for _, co := range cfg.Companies {
		if co.Disabled {
			continue
		}
		wg.Add(1)
		go func(co Company) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			jobs, err := fetchCompany(ctx, co)
			if err != nil {
				log.Printf("[%s] fetch failed: %v", co.Name, err)
				return
			}
			var matched []Job
			for _, j := range jobs {
				if filter.Match(j) {
					matched = append(matched, j)
				}
			}
			// Only jobs passing the title/location filters are remembered, so
			// widening the filters later surfaces jobs that were open but
			// filtered out, instead of treating them as already seen.
			if store != nil {
				fresh, firstRun := store.Record(co.Name, matched)
				if firstRun {
					log.Printf("[%s] first run: remembered %d existing matching jobs; alerts start from the next new posting", co.Name, len(matched))
					return
				}
				matched = fresh
			}
			matched = checkExperience(ctx, co, matched, cfg.Filters.MaxYearsExperience)
			mu.Lock()
			result = append(result, matched...)
			mu.Unlock()
		}(co)
	}
	wg.Wait()
	sort.Slice(result, func(i, k int) bool {
		if result[i].Company != result[k].Company {
			return result[i].Company < result[k].Company
		}
		return result[i].Title < result[k].Title
	})
	return result
}

// fetchCompany runs the company's search once per query and merges the
// results, dropping duplicates. A failed query (e.g. a rate limit) is logged
// and skipped: jobs it would have found are still unseen, so the next run
// alerts them. It fails only when every query fails.
func fetchCompany(ctx context.Context, co Company) ([]Job, error) {
	if len(co.Queries) == 0 {
		return fetchers[co.Source](ctx, co)
	}
	var all []Job
	seen := map[string]bool{}
	var lastErr error
	failed := 0
	for _, q := range co.Queries {
		c := co
		c.Query = q
		jobs, err := fetchers[co.Source](ctx, c)
		if err != nil {
			log.Printf("[%s] query %q failed: %v", co.Name, q, err)
			lastErr = err
			failed++
			continue
		}
		for _, j := range jobs {
			if !seen[j.ID] {
				seen[j.ID] = true
				all = append(all, j)
			}
		}
	}
	if failed == len(co.Queries) {
		return nil, fmt.Errorf("all %d queries failed, last: %w", failed, lastErr)
	}
	return all, nil
}

// checkExperience fills in each job's required years (fetching the
// description when the listing lacked it) and drops jobs asking for more
// than maxYears. Jobs whose description can't be read or states no number
// are kept, so a parsing miss never hides a job.
func checkExperience(ctx context.Context, co Company, jobs []Job, maxYears int) []Job {
	if maxYears <= 0 {
		return jobs
	}
	describe := describers[co.Source]
	kept := jobs[:0]
	dropped := 0
	for _, j := range jobs {
		if j.Description == "" && describe != nil {
			desc, err := describe(ctx, co, j)
			if err != nil {
				log.Printf("[%s] could not read description of %q: %v", co.Name, j.Title, err)
			}
			j.Description = desc
		}
		j.Years, j.YearsKnown = RequiredYears(j.Title, j.Description)
		if j.YearsKnown && j.Years > maxYears {
			dropped++
			continue
		}
		kept = append(kept, j)
	}
	if dropped > 0 {
		log.Printf("[%s] skipped %d job(s) asking for more than %d years", co.Name, dropped, maxYears)
	}
	return kept
}
