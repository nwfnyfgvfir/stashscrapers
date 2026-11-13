// Transport-only model subset for the extracted mapping core. No Stash DB.
package models

type ScrapedTag struct {
	Name string `json:"name"`
}
type ScrapedStudio struct {
	Name    string   `json:"name"`
	URL     *string  `json:"url,omitempty"`
	URLs    []string `json:"urls,omitempty"`
	Image   *string  `json:"image,omitempty"`
	Details *string  `json:"details,omitempty"`
	Aliases *string  `json:"aliases,omitempty"`
}
type ScrapedPerformer struct {
	Name           *string       `json:"name,omitempty"`
	Disambiguation *string       `json:"disambiguation,omitempty"`
	Gender         *string       `json:"gender,omitempty"`
	URL            *string       `json:"url,omitempty"`
	URLs           []string      `json:"urls,omitempty"`
	Twitter        *string       `json:"twitter,omitempty"`
	Birthdate      *string       `json:"birthdate,omitempty"`
	Ethnicity      *string       `json:"ethnicity,omitempty"`
	Country        *string       `json:"country,omitempty"`
	EyeColor       *string       `json:"eye_color,omitempty"`
	Height         *string       `json:"height,omitempty"`
	Measurements   *string       `json:"measurements,omitempty"`
	FakeTits       *string       `json:"fake_tits,omitempty"`
	PenisLength    *string       `json:"penis_length,omitempty"`
	Circumcised    *string       `json:"circumcised,omitempty"`
	CareerLength   *string       `json:"career_length,omitempty"`
	CareerStart    *string       `json:"career_start,omitempty"`
	CareerEnd      *string       `json:"career_end,omitempty"`
	Tattoos        *string       `json:"tattoos,omitempty"`
	Piercings      *string       `json:"piercings,omitempty"`
	Aliases        *string       `json:"aliases,omitempty"`
	Image          *string       `json:"image,omitempty"`
	Images         []string      `json:"images,omitempty"`
	Details        *string       `json:"details,omitempty"`
	DeathDate      *string       `json:"death_date,omitempty"`
	HairColor      *string       `json:"hair_color,omitempty"`
	Weight         *string       `json:"weight,omitempty"`
	Tags           []*ScrapedTag `json:"tags,omitempty"`
}
type ScrapedScene struct {
	Title          *string             `json:"title,omitempty"`
	Code           *string             `json:"code,omitempty"`
	Details        *string             `json:"details,omitempty"`
	Director       *string             `json:"director,omitempty"`
	URL            *string             `json:"url,omitempty"`
	URLs           []string            `json:"urls,omitempty"`
	Date           *string             `json:"date,omitempty"`
	ProductionDate *string             `json:"production_date,omitempty"`
	Image          *string             `json:"image,omitempty"`
	Duration       *int                `json:"duration,omitempty"`
	Performers     []*ScrapedPerformer `json:"performers,omitempty"`
	Tags           []*ScrapedTag       `json:"tags,omitempty"`
	Studio         *ScrapedStudio      `json:"studio,omitempty"`
	Movies         []*ScrapedMovie     `json:"movies,omitempty"`
	Groups         []*ScrapedGroup     `json:"groups,omitempty"`
}
type ScrapedImage struct {
	Title        *string
	Code         *string
	Details      *string
	Photographer *string
	URLs         []string
	Date         *string
	Performers   []*ScrapedPerformer
	Tags         []*ScrapedTag
	Studio       *ScrapedStudio
}
type ScrapedGallery struct {
	Title        *string
	Code         *string
	Details      *string
	Photographer *string
	URL          *string
	URLs         []string
	Date         *string
	Performers   []*ScrapedPerformer
	Tags         []*ScrapedTag
	Studio       *ScrapedStudio
}
type ScrapedGroup struct {
	Name       *string
	Aliases    *string
	URL        *string
	URLs       []string
	Duration   *string
	Date       *string
	Director   *string
	Synopsis   *string
	FrontImage *string
	BackImage  *string
	Studio     *ScrapedStudio
	Tags       []*ScrapedTag
}
type ScrapedMovie = ScrapedGroup
