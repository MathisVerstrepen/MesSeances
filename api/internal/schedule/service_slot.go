package schedule

import (
	"sort"
	"strings"
	"time"
)

func (s *Service) SearchSlot(query SlotQuery) ([]SlotResult, error) {
	city := strings.TrimSpace(query.City)
	hasTheaters := len(query.TheaterIDs) > 0
	if city != "" && hasTheaters {
		return nil, invalid("Les paramètres city et theaters sont mutuellement exclusifs.")
	}
	if city == "" && !hasTheaters {
		return nil, invalid("Le paramètre city ou theaters est requis.")
	}
	date, err := s.parseDate(query.Date)
	if err != nil {
		return nil, err
	}
	start, err := s.parseServiceClock(date, query.StartAfter, "start_after")
	if err != nil {
		return nil, err
	}
	finish, err := s.parseServiceClock(date, query.FinishBefore, "finish_before")
	if err != nil {
		return nil, err
	}
	if !finish.After(start) {
		return nil, invalid("Le paramètre finish_before doit être postérieur à start_after.")
	}
	if query.BufferAds < 0 || query.BufferAds > 120 {
		return nil, invalid("Le paramètre buffer_ads doit être un entier compris entre 0 et 120.")
	}
	if err := validateLanguage(query.Language); err != nil {
		return nil, err
	}
	if err := validateSlotFormat(query.Format); err != nil {
		return nil, err
	}
	view := s.source.Snapshot()
	selected, err := s.selectTheaters(view, query.TheaterIDs, city, false)
	if err != nil {
		return nil, err
	}
	results := []SlotResult{}
	for _, theaterPosition := range selected {
		theater := view.data.Theaters[theaterPosition]
		for _, showingPosition := range view.theaterDate[theaterDateKey{theaterID: theater.ID, date: query.Date}] {
			record := view.data.Showtimes[showingPosition]
			if !matchesLanguage(record.Language, query.Language) || !matchesFormat(record.Format, query.Format) {
				continue
			}
			showtime := materializeRecord(record)
			effectiveStart := showtime.StartTime
			if !query.IncludeAds {
				effectiveStart = effectiveStart.Add(time.Duration(query.BufferAds) * time.Minute)
			}
			effectiveEnd := showtime.EndTime.Add(time.Duration(query.BufferAds) * time.Minute)
			if effectiveStart.Before(start) || effectiveEnd.After(finish) {
				continue
			}
			poster, backdrop := materializeMovieMedia(record.Movie)
			results = append(results, SlotResult{Showtime: showtime, Theater: TheaterSummary{Provider: recordProvider(theater.Provider, theater.ID), ID: theater.ID, Name: theater.Name, City: theater.City}, EffectiveStartTime: effectiveStart, EffectiveEndTime: effectiveEnd, BufferAdsMinutes: query.BufferAds, SlackBeforeMinutes: int(effectiveStart.Sub(start) / time.Minute), SlackAfterMinutes: int(finish.Sub(effectiveEnd) / time.Minute), PosterURL: poster, BackdropURL: backdrop})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if !results[i].Showtime.StartTime.Equal(results[j].Showtime.StartTime) {
			return results[i].Showtime.StartTime.Before(results[j].Showtime.StartTime)
		}
		if results[i].Theater.Name != results[j].Theater.Name {
			return results[i].Theater.Name < results[j].Theater.Name
		}
		return results[i].Showtime.ID < results[j].Showtime.ID
	})
	return results, nil
}
