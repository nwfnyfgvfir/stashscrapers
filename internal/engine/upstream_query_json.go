// Derived from stashapp/stash 47057616fe9da0cf38cfa8c39ab160e5441eeafc, pkg/scraper/json.go.
// SPDX-License-Identifier: AGPL-3.0-only
package engine

import (
	"context"
	"github.com/tidwall/gjson"
	"stashscrapers/internal/engine/logger"
)

type jsonQuery struct {
	doc       string
	scraper   *jsonScraper
	queryType QueryType
	url       string
}

func (q *jsonQuery) getType() QueryType {
	return q.queryType
}

func (q *jsonQuery) setType(t QueryType) {
	q.queryType = t
}

func (q *jsonQuery) getURL() string {
	return q.url
}

func (q *jsonQuery) runQuery(selector string) ([]string, error) {
	value := gjson.Get(q.doc, selector)

	if !value.Exists() {
		// many possible reasons why the selector may not be in the json object
		// and not all are errors.
		// Just return nil
		return nil, nil
	}

	var ret []string
	if value.IsArray() {
		value.ForEach(func(k, v gjson.Result) bool {
			ret = append(ret, v.String())
			return true
		})
	} else {
		ret = append(ret, value.String())
	}

	return ret, nil
}

func (q *jsonQuery) subScrape(ctx context.Context, value string) mappedQuery {
	doc, err := q.scraper.loadURL(ctx, value)

	if err != nil {
		logger.Warnf("Error getting URL '%s' for sub-scraper: %s", value, err.Error())
		return nil
	}

	return q.scraper.getJsonQuery(doc, value)
}
