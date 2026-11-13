package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/antchfx/xpath"
	"github.com/dop251/goja"
	"golang.org/x/net/html"
)

const UpstreamCommit = "47057616fe9da0cf38cfa8c39ab160e5441eeafc"
const Version = "0.1.0+stash.4705761"

var ErrInput = errors.New("invalid scraper input")

type Operation struct {
	Object       string   `json:"object"`
	Operation    string   `json:"operation"`
	Action       string   `json:"action"`
	URLs         []string `json:"urls,omitempty"`
	Script       []string `json:"script,omitempty"`
	QueryURL     string   `json:"-"`
	action       ActionDefinition
	replacements queryURLReplacements
}

type Rule struct{ Definition *Definition }

func LoadReader(id string, reader io.Reader) (*Rule, error) {
	d, e := loadConfigFromYAML(id, reader)
	if e != nil {
		return nil, e
	}
	r := &Rule{d}
	if e = r.Validate(); e != nil {
		return nil, e
	}
	return r, nil
}
func Load(path string) (*Rule, error) {
	d, err := loadConfigFromYAMLFile(path)
	if err != nil {
		return nil, err
	}
	r := &Rule{d}
	if err = r.Validate(); err != nil {
		return nil, err
	}
	return r, nil
}
func (r *Rule) Name() string                  { return r.Definition.Name }
func (r *Rule) Driver() *scraperDriverOptions { return r.Definition.DriverOptions }
func (r *Rule) Operations() []Operation {
	d := r.Definition
	ret := []Operation{}
	add := func(obj, op string, a ActionDefinition, query string, reps queryURLReplacements, urls []string) {
		ret = append(ret, Operation{Object: obj, Operation: op, Action: string(a.Action), URLs: urls, Script: a.Script, QueryURL: query, action: a, replacements: reps})
	}
	if a := d.SceneByName; a != nil {
		add("scene", "name", a.ActionDefinition, a.QueryURL, nil, nil)
	}
	if a := d.SceneByQueryFragment; a != nil {
		add("scene", "query_fragment", a.ActionDefinition, a.QueryURL, a.QueryURLReplacements, nil)
	}
	if a := d.SceneByFragment; a != nil {
		add("scene", "fragment", a.ActionDefinition, a.QueryURL, a.QueryURLReplacements, nil)
	}
	for _, a := range d.SceneByURL {
		add("scene", "url", a.ActionDefinition, a.QueryURL, a.QueryURLReplacements, a.URL)
	}
	if a := d.PerformerByName; a != nil {
		add("performer", "name", a.ActionDefinition, a.QueryURL, nil, nil)
	}
	if a := d.PerformerByFragment; a != nil {
		add("performer", "fragment", a.ActionDefinition, a.QueryURL, a.QueryURLReplacements, nil)
	}
	for _, a := range d.PerformerByURL {
		add("performer", "url", a.ActionDefinition, a.QueryURL, a.QueryURLReplacements, a.URL)
	}
	return ret
}
func (r *Rule) HasStashServer() bool { return r.Definition.StashServer != nil }
func (r *Rule) TrustedHost(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, op := range r.Operations() {
		for _, pattern := range append(append([]string{}, op.URLs...), op.QueryURL) {
			if strings.HasPrefix(pattern, "{") {
				continue
			}
			if !strings.Contains(pattern, "://") {
				pattern = "https://" + pattern
			}
			p, e := url.Parse(pattern)
			if e == nil && p.Hostname() != "" {
				expected := strings.ToLower(p.Hostname())
				if host == expected || strings.HasSuffix(host, "."+expected) {
					return true
				}
			}
		}
	}
	return false
}
func (r *Rule) Select(obj, op, rawURL string) (Operation, error) {
	for _, a := range r.Operations() {
		if a.Object != obj || a.Operation != op {
			continue
		}
		if op == "url" {
			found := false
			for _, pattern := range a.URLs {
				if strings.Contains(rawURL, pattern) && safeMatchHost(rawURL, pattern) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		return a, nil
	}
	return Operation{}, errors.New("operation or URL is not supported by this rule")
}
func safeMatchHost(raw, pattern string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if !strings.Contains(pattern, "://") {
		pattern = "https://" + pattern
	}
	p, err := url.Parse(pattern)
	if err != nil || p.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	expected := strings.ToLower(p.Hostname())
	return host == expected || strings.HasSuffix(host, "."+expected)
}

func (r *Rule) Validate() error {
	for _, a := range r.Operations() {
		for _, replacements := range a.replacements {
			for _, re := range replacements {
				if _, err := regexp.Compile(re.Regex); err != nil {
					return fmt.Errorf("invalid queryURLReplace regex: %w", err)
				}
			}
		}
		if a.Action == "script" || a.Action == "stash" {
			continue
		}
		maps := r.Definition.XPathScrapers
		if a.Action == "scrapeJson" {
			maps = r.Definition.JsonScrapers
		}
		m, ok := maps[a.action.Scraper]
		if !ok {
			return fmt.Errorf("missing mapping %s", a.action.Scraper)
		}
		if a.Object == "scene" && m.Scene == nil || a.Object == "performer" && m.Performer == nil {
			return fmt.Errorf("missing %s mapping", a.Object)
		}
	}
	for kind, all := range map[string]mappedScrapers{"xpath": r.Definition.XPathScrapers, "json": r.Definition.JsonScrapers} {
		for _, m := range all {
			configs := []mappedConfig{}
			if m.Scene != nil {
				configs = append(configs, m.Scene.mappedConfig, m.Scene.Tags, m.Scene.Studio, m.Scene.Performers.mappedConfig, m.Scene.Performers.Tags, m.Scene.Movies, m.Scene.Groups)
			}
			if m.Performer != nil {
				configs = append(configs, m.Performer.mappedConfig, m.Performer.Tags)
			}
			for _, cfg := range configs {
				for _, attr := range cfg {
					if err := validateAttr(attr, m.Common, kind); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
func validateAttr(a mappedScraperAttrConfig, common commonMappedConfig, kind string) error {
	if a.Selector != "" && kind == "xpath" {
		selector := mappedConfig{}.applyCommon(common, a.Selector)
		selector = strings.NewReplacer("{inputURL}", "https://example.com/", "{inputHostname}", "example.com").Replace(selector)
		if _, err := xpath.Compile(selector); err != nil {
			return fmt.Errorf("invalid XPath: %w", err)
		}
	}
	for _, p := range a.postProcessActions {
		switch v := p.(type) {
		case *postProcessReplace:
			for _, re := range *v {
				if _, err := regexp.Compile(re.Regex); err != nil {
					return err
				}
			}
		case *postProcessJavascript:
			if _, err := goja.Compile("", "(function(){"+string(*v)+"})()", true); err != nil {
				return err
			}
		case *postProcessSubScraper:
			if err := validateAttr(mappedScraperAttrConfig(*v), nil, kind); err != nil {
				return err
			}
		}
	}
	return nil
}

type FetchFunc func(context.Context, string) (io.ReadCloser, error)
type ScriptFunc func(context.Context, []string, map[string]interface{}) (interface{}, error)
type RunOptions struct {
	Fetch  FetchFunc
	Script ScriptFunc
}

func (r *Rule) Run(ctx context.Context, obj string, a Operation, input map[string]interface{}, opt RunOptions) ([]map[string]interface{}, error) {
	if a.Action == "stash" {
		return nil, errors.New("full Stash API is required")
	}
	if a.Action == "script" {
		if opt.Script == nil {
			return nil, errors.New("script runtime is not configured")
		}
		out, err := opt.Script(ctx, a.Script, input)
		if err != nil {
			return nil, err
		}
		return toMaps(out, a.Operation == "name")
	}
	if obj == "performer" && (a.Operation == "fragment" || a.Operation == "query_fragment") {
		return nil, errors.New("upstream mapped performer fragment action is unsupported")
	}
	target := a.QueryURL
	if a.Operation == "name" {
		name, _ := input["name"].(string)
		target = strings.ReplaceAll(target, "{}", url.QueryEscape(name))
	} else {
		if a.Operation == "url" {
			target, _ = input["url"].(string)
			// Upstream URL rewrite is conditional on queryURLReplace.
			if a.replacements != nil {
				target = a.QueryURL
			}
		}
		params := map[string]string{}
		for k, v := range input {
			if s, ok := v.(string); ok {
				params[k] = s
			}
		}
		if urls, ok := input["urls"].([]interface{}); ok && len(urls) > 0 {
			if s, ok := urls[0].(string); ok {
				params["url"] = s
			}
		}
		for k, v := range params {
			if re, ok := a.replacements[k]; ok {
				v = re.apply(v)
			}
			target = strings.ReplaceAll(target, "{"+k+"}", v)
		}
		if regexp.MustCompile(`\{[a-z_]+\}`).MatchString(target) {
			return nil, fmt.Errorf("%w: fragment is missing queryURL parameters", ErrInput)
		}
	}
	var fetchErr error
	fetch := func(c context.Context, u string) (io.ReadCloser, error) {
		body, err := opt.Fetch(c, u)
		if err != nil {
			fetchErr = err
		}
		return body, err
	}
	maps := r.Definition.XPathScrapers
	var q mappedQuery
	if a.Action == "scrapeXPath" {
		s := &xpathScraper{fetch: fetch}
		doc, err := s.loadURL(ctx, target)
		if err != nil {
			return nil, err
		}
		q = s.getXPathQuery(doc, target)
	} else if a.Action == "scrapeJson" {
		maps = r.Definition.JsonScrapers
		s := &jsonScraper{fetch: fetch}
		doc, err := s.loadURL(ctx, target)
		if err != nil {
			return nil, err
		}
		q = s.getJsonQuery(doc, target)
	} else {
		return nil, errors.New("unknown action")
	}
	m := maps[a.action.Scraper]
	search := a.Operation == "name"
	var value interface{}
	var err error
	if search {
		q.setType(SearchQuery)
	}
	if obj == "scene" {
		if search {
			value, err = m.scrapeScenes(ctx, q)
		} else {
			value, err = m.scrapeScene(ctx, q)
		}
	} else {
		if search {
			value, err = m.scrapePerformers(ctx, q)
		} else {
			value, err = m.scrapePerformer(ctx, q)
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if fetchErr != nil {
		return nil, fetchErr
	}
	if err != nil {
		return nil, err
	}
	return toMaps(value, search)
}
func toMaps(v interface{}, list bool) ([]map[string]interface{}, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(b) == "null" {
		return []map[string]interface{}{}, nil
	}
	var out []map[string]interface{}
	if list {
		err = json.Unmarshal(b, &out)
	} else {
		var one map[string]interface{}
		err = json.Unmarshal(b, &one)
		if len(one) > 0 {
			out = append(out, one)
		}
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, err
}

type xpathScraper struct{ fetch FetchFunc }

func (s *xpathScraper) loadURL(ctx context.Context, u string) (*html.Node, error) {
	b, e := s.fetch(ctx, u)
	if e != nil {
		return nil, e
	}
	defer b.Close()
	return html.Parse(b)
}
func (s *xpathScraper) getXPathQuery(d *html.Node, u string) *xpathQuery {
	return &xpathQuery{doc: d, scraper: s, url: u}
}

type jsonScraper struct{ fetch FetchFunc }

func (s *jsonScraper) loadURL(ctx context.Context, u string) (string, error) {
	b, e := s.fetch(ctx, u)
	if e != nil {
		return "", e
	}
	defer b.Close()
	raw, e := io.ReadAll(b)
	if e != nil {
		return "", e
	}
	if !json.Valid(raw) {
		return "", errors.New("response is not valid JSON")
	}
	return string(raw), nil
}
func (s *jsonScraper) getJsonQuery(d, u string) *jsonQuery {
	return &jsonQuery{doc: d, scraper: s, url: u}
}
