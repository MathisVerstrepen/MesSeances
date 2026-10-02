package enrichment

import (
	"context"

	"messeances/api/internal/tmdb"
	"messeances/api/internal/wikidata"
)

type metacriticLookup interface {
	MetacriticID(context.Context, string) (string, error)
}

// MetacriticProvider shares the raw TMDB client while decorating only Details.
type MetacriticProvider struct {
	*tmdb.Client
	lookup metacriticLookup
}

func NewMetacriticProvider(client *tmdb.Client, lookup metacriticLookup) *MetacriticProvider {
	return &MetacriticProvider{Client: client, lookup: lookup}
}

func (p *MetacriticProvider) Details(ctx context.Context, id int64) (tmdb.Details, error) {
	details, err := p.Client.Details(ctx, id)
	if ctx.Err() != nil {
		return tmdb.Details{}, ctx.Err()
	}
	if err != nil {
		return tmdb.Details{}, err
	}
	if p.lookup == nil || !wikidata.ValidQID(details.WikidataID) {
		return details, nil
	}
	value, err := p.lookup.MetacriticID(ctx, details.WikidataID)
	if ctx.Err() != nil {
		return tmdb.Details{}, ctx.Err()
	}
	if err == nil && (value == "" || wikidata.ValidMetacriticID(value)) {
		details.MetacriticID = value
		details.MetacriticChecked = true
	}
	return details, nil
}
