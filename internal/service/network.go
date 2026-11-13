package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html/charset"
	"stashscrapers/internal/engine"
)

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *APIError) Error() string                    { return e.Message }
func failure(code, message string, status int) error { return &APIError{code, message, status} }
func validateURL(raw string, private bool) error {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("only absolute HTTP(S) URLs without credentials or fragments are allowed")
	}
	if !private {
		if strings.EqualFold(u.Hostname(), "localhost") {
			return errors.New("private target is blocked")
		}
		if ip := net.ParseIP(u.Hostname()); ip != nil && blockedIP(ip) {
			return errors.New("private target is blocked")
		}
	}
	return nil
}
func blockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || !ip.IsGlobalUnicast()
}
func validateHost(ctx context.Context, host string) error {
	ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
	if e != nil {
		return e
	}
	if len(ips) == 0 {
		return errors.New("host has no addresses")
	}
	for _, ip := range ips {
		if blockedIP(ip.IP) {
			return errors.New("private target address is blocked")
		}
	}
	return nil
}

type limiter struct {
	mu   sync.Mutex
	next map[string]time.Time
}

func (l *limiter) wait(ctx context.Context, host string, interval int) error {
	host = strings.ToLower(host)
	l.mu.Lock()
	if l.next == nil {
		l.next = map[string]time.Time{}
	}
	now := time.Now()
	when := l.next[host]
	if when.Before(now) {
		when = now
	}
	l.next[host] = when.Add(time.Duration(interval) * time.Millisecond)
	l.mu.Unlock()
	t := time.NewTimer(time.Until(when))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func targetClient(c Config) (*http.Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConns = 16
	tr.MaxConnsPerHost = c.Concurrency
	tr.Proxy = nil
	if c.Proxy != "" {
		p, e := url.Parse(c.Proxy)
		if e != nil {
			return nil, e
		}
		tr.Proxy = http.ProxyURL(p)
	}
	if !c.AllowPrivateTargets && c.Proxy == "" {
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, e := net.SplitHostPort(addr)
			if e != nil {
				return nil, e
			}
			ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
			if e != nil {
				return nil, e
			}
			for _, ip := range ips {
				if blockedIP(ip.IP) {
					return nil, errors.New("private target address is blocked")
				}
			}
			if len(ips) == 0 {
				return nil, errors.New("host has no addresses")
			}
			d := net.Dialer{Timeout: 10 * time.Second}
			return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		}
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Transport: tr, Jar: jar, Timeout: time.Duration(c.TimeoutSeconds) * time.Second}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if e := validateURL(req.URL.String(), c.AllowPrivateTargets); e != nil {
			return e
		}
		if !c.AllowPrivateTargets {
			if e := validateHost(req.Context(), req.URL.Hostname()); e != nil {
				return e
			}
		}
		if len(via) > 0 && via[0].URL.Hostname() != req.URL.Hostname() {
			req.Header.Del("Cookie")
			req.Header.Del("Authorization")
			for k := range req.Header {
				if k != "User-Agent" && k != "Accept" && k != "Accept-Language" {
					req.Header.Del(k)
				}
			}
		}
		return nil
	}
	return client, nil
}
func (a *App) fetcher(c Config, rule *engine.Rule, settings ScraperConfig) engine.FetchFunc {
	client, _ := targetClient(c)
	// Each API call owns its client; release idle sockets when its context ends.
	if a.targetTransport != nil {
		client.Transport = a.targetTransport
	}
	driver := rule.Driver()
	if driver != nil {
		for _, group := range driver.Cookies {
			u, e := url.Parse(group.CookieURL)
			if e != nil {
				continue
			}
			var cookies []*http.Cookie
			for _, v := range group.Cookies {
				if v.Value != "" {
					cookies = append(cookies, &http.Cookie{Name: v.Name, Value: v.Value, Domain: v.Domain, Path: v.Path})
				}
			}
			client.Jar.SetCookies(u, cookies)
		}
	}
	return func(ctx context.Context, raw string) (io.ReadCloser, error) {
		context.AfterFunc(ctx, client.CloseIdleConnections)
		if e := validateURL(raw, c.AllowPrivateTargets); e != nil {
			return nil, failure("input_error", e.Error(), 400)
		}
		u, _ := url.Parse(raw)
		if !c.AllowPrivateTargets {
			if e := validateHost(ctx, u.Hostname()); e != nil {
				return nil, failure("input_error", "target DNS is unavailable or points to a private network", 400)
			}
		}
		if driver != nil && driver.UseCDP {
			if e := a.limiter.wait(ctx, u.Hostname(), c.SiteIntervalMS); e != nil {
				return nil, e
			}
			return a.fetchCDP(ctx, c, rule, settings, raw)
		}
		for attempt := 0; attempt < 2; attempt++ {
			if e := a.limiter.wait(ctx, u.Hostname(), c.SiteIntervalMS); e != nil {
				return nil, e
			}
			req, e := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
			if e != nil {
				return nil, e
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 StashCommunityService/0.1")
			req.Header.Set("Accept", "*/*")
			if rule.TrustedHost(raw) {
				if driver != nil {
					for _, h := range driver.Headers {
						req.Header.Set(h.Key, h.Value)
					}
				}
				for k, v := range settings.Headers {
					req.Header.Set(k, v)
				}
				if settings.Cookie != "" {
					req.Header.Set("Cookie", settings.Cookie)
				}
				if settings.APIKey != "" {
					key := settings.APIKeyHeader
					if key == "" {
						key = "Authorization"
					}
					req.Header.Set(key, settings.APIKey)
				}
			}
			resp, e := client.Do(req)
			if e != nil {
				return nil, failure("network_error", "target request failed; check DNS, proxy and network", 502)
			}
			if (resp.StatusCode == 429 || resp.StatusCode == 502 || resp.StatusCode == 503) && attempt == 0 {
				resp.Body.Close()
				t := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					t.Stop()
					return nil, ctx.Err()
				case <-t.C:
				}
				continue
			}
			if resp.StatusCode == 403 || resp.StatusCode == 401 || resp.StatusCode == 429 {
				resp.Body.Close()
				return nil, failure("target_denied", fmt.Sprintf("target returned HTTP %d", resp.StatusCode), 502)
			}
			if resp.StatusCode == 404 {
				resp.Body.Close()
				return nil, failure("not_found", "target returned HTTP 404", 404)
			}
			if resp.StatusCode >= 400 {
				resp.Body.Close()
				return nil, failure("network_error", fmt.Sprintf("target returned HTTP %d", resp.StatusCode), 502)
			}
			b, e := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024+1))
			resp.Body.Close()
			if e != nil {
				return nil, e
			}
			if len(b) > 8*1024*1024 {
				return nil, failure("parse_error", "target response exceeds 8 MiB", 502)
			}
			reader, e := charset.NewReader(bytes.NewReader(b), resp.Header.Get("Content-Type"))
			if e != nil {
				return nil, e
			}
			decoded, e := io.ReadAll(io.LimitReader(reader, 16*1024*1024+1))
			if e != nil {
				return nil, e
			}
			if len(decoded) > 16*1024*1024 {
				return nil, failure("parse_error", "decoded response is too large", 502)
			}
			return io.NopCloser(bytes.NewReader(decoded)), nil
		}
		return nil, errors.New("request failed")
	}
}
