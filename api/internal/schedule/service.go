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
	Now         func() time.Time
}

type Service struct {
	location *time.Location
	source   Source
	options  ServiceOptions
	now      func() time.Time
}

func NewService(source Source, options ServiceOptions) (*Service, error) {
	if source == nil {
		return nil, fmt.Errorf("schedule source is required")
	}
	location, err := time.LoadLocation(Timezone)
	if err != nil {
		return nil, fmt.Errorf("load schedule timezone: %w", err)
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	options.DefaultCity = strings.TrimSpace(options.DefaultCity)
	return &Service{location: location, source: source, options: options, now: options.Now}, nil
}

func (s *Service) HasSnapshot() bool {
	return s != nil && s.source.Snapshot() != nil
}
