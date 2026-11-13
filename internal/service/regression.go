package service

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"strings"

	"stashscrapers/internal/engine"
)

//go:embed testdata
var fixtures embed.FS

func fixtureVerified(id string) bool {
	return id == "AdultEmpire" || id == "1pondo" || id == "Boobpedia"
}

type fixtureCase struct {
	id, rule, obj, op, target, body, key, value string
	input                                       map[string]interface{}
	count                                       int
}

func regressionCases() []fixtureCase {
	return []fixtureCase{
		{"AdultEmpire", "AdultEmpire.yml", "scene", "name", "https://www.adultempire.com/vod/search?q=fixture+query", "empire-search.html", "title", "Fixture One", map[string]interface{}{"name": "fixture query"}, 2},
		{"AdultEmpire", "AdultEmpire.yml", "scene", "url", "https://www.adultempire.com/10001/fixture-one.html", "empire-detail.html", "title", "Fixture One", map[string]interface{}{"url": "https://www.adultempire.com/10001/fixture-one.html"}, 1},
		{"1pondo", "1pondo.yml", "scene", "url", "https://en.1pondo.tv/dyn/phpauto/movie_details/movie_id/010124_001.json", "1pondo.json", "title", "Fixture JSON title", map[string]interface{}{"url": "https://en.1pondo.tv/movies/010124_001/"}, 1},
		{"Boobpedia", "Boobpedia.yml", "performer", "name", "https://www.boobpedia.com/wiki/api.php?action=query&format=json&list=search&srwhat=title&srsearch=Fixture+Person", "boobpedia-search.json", "name", "Fixture Person", map[string]interface{}{"name": "Fixture Person"}, 2},
		{"Boobpedia", "Boobpedia.yml", "performer", "url", "https://www.boobpedia.com/boobs/Fixture_Person", "boobpedia-detail.html", "name", "Fixture Person", map[string]interface{}{"url": "https://www.boobpedia.com/boobs/Fixture_Person"}, 1},
	}
}
func runRegression(ctx context.Context, s *Snapshot) error {
	for _, test := range regressionCases() {
		raw, e := fixtures.ReadFile("testdata/community/" + test.rule)
		if e != nil {
			return e
		}
		rule, e := engine.LoadReader(test.id, bytes.NewReader(raw))
		if e != nil {
			return e
		}
		for pass := 0; pass < 2; pass++ {
			if pass == 1 {
				entry := s.Entries[test.id]
				if entry == nil {
					continue
				}
				if entry.rule == nil {
					return fmt.Errorf("%s cannot load", test.id)
				}
				rule = entry.rule
			}
			url, _ := test.input["url"].(string)
			op, e := rule.Select(test.obj, test.op, url)
			if e != nil {
				return e
			}
			body, _ := fixtures.ReadFile("testdata/" + test.body)
			fetch := func(_ context.Context, target string) (io.ReadCloser, error) {
				if test.id == "Boobpedia" && test.op == "url" && target == "https://www.boobpedia.com/boobs/File:Portrait.jpg" {
					data, _ := fixtures.ReadFile("testdata/boobpedia-image.html")
					return io.NopCloser(bytes.NewReader(data)), nil
				}
				if target != test.target {
					return nil, fmt.Errorf("%s query URL changed; compatibility review required", test.id)
				}
				return io.NopCloser(bytes.NewReader(body)), nil
			}
			result, e := rule.Run(ctx, test.obj, op, test.input, engine.RunOptions{Fetch: fetch})
			if e != nil {
				return e
			}
			if len(result) != test.count || result[0][test.key] != test.value {
				return fmt.Errorf("%s expected fixture results changed", test.id)
			}
			if test.id == "1pondo" {
				if result[0]["date"] != "2024-01-02" || len(result[0]["performers"].([]interface{})) != 2 {
					return errors.New("JSON nested/date mapping regression")
				}
			}
			if test.id == "Boobpedia" && test.op == "url" {
				if result[0]["career_start"] != "2010" || result[0]["birthdate"] != "1990-01-02" {
					return errors.New("JavaScript/date post-process regression")
				}
				images, _ := result[0]["images"].([]interface{})
				if len(images) != 1 || images[0] != "https://www.boobpedia.com/images/portrait.jpg" {
					return errors.New("subScraper image mapping regression")
				}
			}
		}
	}
	return nil
}
func fixtureBody(name string) string {
	b, _ := fixtures.ReadFile("testdata/" + name)
	return strings.TrimSpace(string(b))
}
