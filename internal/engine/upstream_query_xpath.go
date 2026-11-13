// Derived from stashapp/stash 47057616fe9da0cf38cfa8c39ab160e5441eeafc, pkg/scraper/xpath.go.
// SPDX-License-Identifier: AGPL-3.0-only
package engine

import (
	"context"
	"fmt"
	"github.com/antchfx/htmlquery"
	"golang.org/x/net/html"
	"regexp"
	"stashscrapers/internal/engine/logger"
	"strings"
)

var (
	multiSpaceRE = regexp.MustCompile("  +")
	newlineRE    = regexp.MustCompile("\n")
)

type xpathQuery struct {
	doc       *html.Node
	scraper   *xpathScraper
	queryType QueryType
	url       string
}

func (q *xpathQuery) getType() QueryType {
	return q.queryType
}

func (q *xpathQuery) setType(t QueryType) {
	q.queryType = t
}

func (q *xpathQuery) getURL() string {
	return q.url
}

func (q *xpathQuery) runQuery(selector string) ([]string, error) {
	found, err := htmlquery.QueryAll(q.doc, selector)
	if err != nil {
		return nil, fmt.Errorf("selector '%s': parse error: %v", selector, err)
	}

	var ret []string
	for _, n := range found {
		// don't add empty strings
		nodeText := q.nodeText(n)
		if nodeText != "" {
			ret = append(ret, q.nodeText(n))
		}
	}

	return ret, nil
}

func (q *xpathQuery) nodeText(n *html.Node) string {
	var ret string
	if n != nil && n.Type == html.CommentNode {
		ret = htmlquery.OutputHTML(n, true)
	} else {
		ret = htmlquery.InnerText(n)
	}

	// trim all leading and trailing whitespace
	ret = strings.TrimSpace(ret)

	// remove multiple whitespace
	ret = multiSpaceRE.ReplaceAllString(ret, " ")

	// TODO - make this optional
	ret = newlineRE.ReplaceAllString(ret, "")

	return ret
}

func (q *xpathQuery) subScrape(ctx context.Context, value string) mappedQuery {
	doc, err := q.scraper.loadURL(ctx, value)

	if err != nil {
		logger.Warnf("Error getting URL '%s' for sub-scraper: %s", value, err.Error())
		return nil
	}

	return q.scraper.getXPathQuery(doc, value)
}
