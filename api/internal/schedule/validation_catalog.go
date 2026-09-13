package schedule

import "fmt"

// ValidateCatalogOnlyDataset never weakens provider snapshot validation.
func ValidateCatalogOnlyDataset(data Dataset) error {
	if data.SchemaVersion != SchemaVersion || data.Timezone != Timezone || data.UpcomingCompletedAt.IsZero() || !data.GeneratedAt.Equal(data.UpcomingCompletedAt) || data.Provider != "" || data.Scope != "" || data.Window != (Window{}) || len(data.Theaters) != 0 || len(data.Showtimes) != 0 {
		return fmt.Errorf("invalid catalog-only dataset")
	}
	return validatePublicMovieCatalog(data)
}

func ValidateSnapshotDataset(data Dataset, revision SnapshotRevision) error {
	if revision.ScheduleVersion < 0 || revision.EnrichmentVersion < 0 || revision.TheaterLocationVersion < 0 {
		return fmt.Errorf("invalid snapshot revision")
	}
	if revision.ScheduleVersion == 0 {
		if revision.EnrichmentVersion == 0 {
			return fmt.Errorf("unpublished catalog revision")
		}
		return ValidateCatalogOnlyDataset(data)
	}
	return ValidateDataset(data, true)
}
