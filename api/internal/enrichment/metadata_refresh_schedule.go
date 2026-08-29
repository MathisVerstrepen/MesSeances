package enrichment

import "context"

type MetadataRefreshCompletion struct {
	Succeeded bool
}

type MetadataRefreshClaim func(context.Context) (bool, error)

// StartScheduled reserves the same manager and TMDB gate as Start, then claims
// durable occurrence identity before publishing status or launching work.
func (m *MetadataRefreshManager) StartScheduled(claim MetadataRefreshClaim) (<-chan MetadataRefreshCompletion, error) {
	if m == nil || claim == nil {
		return nil, ErrMetadataRefreshUnavailable
	}
	_, completion, err := m.start(claim)
	return completion, err
}
