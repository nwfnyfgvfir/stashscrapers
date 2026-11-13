package service

import "stashscrapers/internal/engine"

func engineRunOptions(a *App, s *Snapshot, e *Entry, c Config) engine.RunOptions {
	return engine.RunOptions{Fetch: a.fetcher(c, e.rule, c.Scrapers[e.ID]), Script: a.scriptRunner(s, e, c)}
}
