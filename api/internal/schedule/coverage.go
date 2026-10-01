package schedule

import (
	"fmt"
	"sort"
)

// PublicationCoverage is acquisition evidence, never reconstructed from a snapshot.
type PublicationCoverage struct {
	TheaterID   string
	ServiceDate string
	Status      string
	Basis       string
}

const (
	CoverageComplete         = "complete"
	CoverageUnknown          = "unknown"
	CoverageDateResponse     = "date_response"
	CoverageAcceptedOmission = "accepted_omission"
	CoverageUnproven         = "unproven"
)

// ProgramCoverage requires each advertised movie/date response. Missing accepted
// responses invalidate the date, even when another film has positive sessions.
func ProgramCoverage(theater string, program map[string][]string, unknown map[string]bool) []PublicationCoverage {
	seen := map[string]bool{}
	result := []PublicationCoverage{}
	for _, dates := range program {
		for _, date := range dates {
			if seen[date] {
				continue
			}
			seen[date] = true
			status, basis := CoverageComplete, CoverageDateResponse
			if unknown[date] {
				status, basis = CoverageUnknown, CoverageAcceptedOmission
			}
			result = append(result, PublicationCoverage{TheaterID: theater, ServiceDate: date, Status: status, Basis: basis})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ServiceDate < result[j].ServiceDate })
	return result
}

func validateCoverage(data Dataset, theaters map[string]TheaterRecord) error {
	seen := map[string]bool{}
	for _, c := range data.Coverage {
		t, ok := theaters[c.TheaterID]
		_, err := ParseServiceDate(c.ServiceDate)
		key := c.TheaterID + "\x00" + c.ServiceDate
		provider := recordProvider(t.Provider, t.ID)
		if !ok || err != nil || c.ServiceDate < data.Window.From || c.ServiceDate > data.Window.Through || seen[key] || data.Provider != ProviderCombined && provider != data.Provider {
			return fmt.Errorf("invalid publication coverage ownership or date")
		}
		if c.Status != CoverageComplete && c.Status != CoverageUnknown || c.Status == CoverageComplete && c.Basis != CoverageDateResponse || c.Status == CoverageUnknown && c.Basis != CoverageUnproven && c.Basis != CoverageAcceptedOmission {
			return fmt.Errorf("invalid publication coverage status or basis")
		}
		if c.Status == CoverageComplete && provider != ProviderUGC && provider != ProviderPathe && provider != ProviderCGR && provider != ProviderGrandEcran && provider != ProviderNoeCinemas {
			return fmt.Errorf("provider has no complete date evidence")
		}
		seen[key] = true
	}
	return nil
}
