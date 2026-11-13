package engine

type scraperAction string

const (
	scraperActionScript scraperAction = "script"
	scraperActionStash  scraperAction = "stash"
	scraperActionXPath  scraperAction = "scrapeXPath"
	scraperActionJson   scraperAction = "scrapeJson"
)

func (a scraperAction) IsValid() bool {
	return a == scraperActionScript || a == scraperActionStash || a == scraperActionXPath || a == scraperActionJson
}

type QueryType int

const SearchQuery QueryType = 1

type ScrapeContentType string

const (
	ScrapeContentTypeGallery   ScrapeContentType = "GALLERY"
	ScrapeContentTypeMovie     ScrapeContentType = "MOVIE"
	ScrapeContentTypeGroup     ScrapeContentType = "GROUP"
	ScrapeContentTypePerformer ScrapeContentType = "PERFORMER"
	ScrapeContentTypeScene     ScrapeContentType = "SCENE"
	ScrapeContentTypeImage     ScrapeContentType = "IMAGE"
)

type ScrapeType string

const (
	ScrapeTypeName     ScrapeType = "NAME"
	ScrapeTypeFragment ScrapeType = "FRAGMENT"
	ScrapeTypeURL      ScrapeType = "URL"
)

type ScraperSpec struct {
	Urls             []string
	SupportedScrapes []ScrapeType
}
type Scraper struct {
	ID, Name                                       string
	Performer, Scene, Gallery, Image, Group, Movie *ScraperSpec
}
type queryURLReplacements map[string]mappedRegexConfigs
