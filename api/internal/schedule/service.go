package schedule

import (
	"fmt"
	"strings"
	"time"
)

const dateLayout = "2006-01-02"

const (
	defaultMovieCatalogPageSize = 24
	maxMovieCatalogPageSize     = 100
)

type ServiceOptions struct {
	DefaultCity string
	CityAliases map[string][]string
}

type Service struct {
	location *time.Location
	source   Source
	options  ServiceOptions
}

func NewService(source Source, options ServiceOptions) (*Service, error) {
	if source == nil {
		return nil, fmt.Errorf("schedule source is required")
	}
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return nil, fmt.Errorf("load schedule timezone: %w", err)
	}
	options.DefaultCity = strings.TrimSpace(options.DefaultCity)
	return &Service{location: location, source: source, options: options}, nil
}
