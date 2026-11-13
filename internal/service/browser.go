package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	cdpfetch "github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"stashscrapers/internal/engine"
)

func (a *App) fetchCDP(ctx context.Context, c Config, rule *engine.Rule, settings ScraperConfig, raw string) (io.ReadCloser, error) {
	if c.CDP == "" {
		return nil, failure("missing_dependency", "CDP browser endpoint is not configured", 424)
	}
	// A browser can create subrequests outside the Go transport; require explicit trust of its network.
	if !c.AllowPrivateTargets {
		return nil, failure("needs_configuration", "CDP requires allow_private_targets acknowledgement for the browser network", 424)
	}
	allocator, cancel := chromedp.NewRemoteAllocator(ctx, c.CDP)
	defer cancel()
	browser, cancelBrowser := chromedp.NewContext(allocator)
	defer cancelBrowser()
	headers := map[string]string{}
	driver := rule.Driver()
	if driver != nil {
		for _, h := range driver.Headers {
			headers[h.Key] = h.Value
		}
	}
	for k, v := range settings.Headers {
		headers[k] = v
	}
	if settings.Cookie != "" {
		headers["Cookie"] = settings.Cookie
	}
	if settings.APIKey != "" {
		key := settings.APIKeyHeader
		if key == "" {
			key = "Authorization"
		}
		headers[key] = settings.APIKey
	}
	managed := map[string]bool{}
	for key := range headers {
		if !strings.EqualFold(key, "Cookie") {
			managed[strings.ToLower(key)] = true
		}
	}
	// Apply credentials only to trusted rule hosts, including redirects and
	// subresources. Global ExtraHTTPHeaders would leak keys to third-party assets.
	chromedp.ListenTarget(browser, func(event interface{}) {
		paused, ok := event.(*cdpfetch.EventRequestPaused)
		if !ok {
			return
		}
		values := []*cdpfetch.HeaderEntry{}
		for key, value := range paused.Request.Headers {
			if !managed[strings.ToLower(key)] {
				values = append(values, &cdpfetch.HeaderEntry{Name: key, Value: fmt.Sprint(value)})
			}
		}
		if rule.TrustedHost(paused.Request.URL) {
			for key, value := range headers {
				if !strings.EqualFold(key, "Cookie") {
					values = append(values, &cdpfetch.HeaderEntry{Name: key, Value: value})
				}
			}
		}
		go func() { _ = chromedp.Run(browser, cdpfetch.ContinueRequest(paused.RequestID).WithHeaders(values)) }()
	})
	tasks := chromedp.Tasks{network.Enable(), cdpfetch.Enable()}
	if rule.TrustedHost(raw) {
		u, _ := url.Parse(raw)
		for key, value := range headers {
			if strings.EqualFold(key, "Cookie") {
				req := &http.Request{Header: http.Header{"Cookie": []string{value}}}
				for _, cookie := range req.Cookies() {
					tasks = append(tasks, network.SetCookie(cookie.Name, cookie.Value).WithDomain(u.Hostname()).WithPath("/"))
				}
			}
		}
	}
	if driver != nil {
		for _, g := range driver.Cookies {
			for _, cookie := range g.Cookies {
				if cookie.Value != "" {
					tasks = append(tasks, network.SetCookie(cookie.Name, cookie.Value).WithDomain(cookie.Domain).WithPath(cookie.Path))
				}
			}
		}
	}
	tasks = append(tasks, chromedp.Navigate(raw))
	if driver != nil {
		if driver.Sleep > 0 {
			tasks = append(tasks, chromedp.Sleep(time.Duration(driver.Sleep)*time.Second))
		}
		for _, click := range driver.Clicks {
			tasks = append(tasks, chromedp.Click(click.XPath, chromedp.BySearch))
			if click.Sleep > 0 {
				tasks = append(tasks, chromedp.Sleep(time.Duration(click.Sleep)*time.Second))
			}
		}
	}
	var html string
	tasks = append(tasks, chromedp.OuterHTML("html", &html, chromedp.ByQuery))
	if err := chromedp.Run(browser, tasks); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, failure("browser_error", "CDP connection or page execution failed", 502)
	}
	if len(html) > 8<<20 {
		return nil, errors.New("CDP page exceeds size limit")
	}
	return io.NopCloser(strings.NewReader(html)), nil
}
