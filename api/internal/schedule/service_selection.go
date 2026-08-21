package schedule

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Service) selectedTheaters(view *SnapshotView, ids []string) ([]int, error) {
	return s.selectTheaters(view, ids, "", true)
}

func (s *Service) selectTheaters(view *SnapshotView, ids []string, city string, useDefault bool) ([]int, error) {
	if len(ids) > 0 {
		selected := make([]int, 0, len(ids))
		seen := make(map[int]bool, len(ids))
		for _, id := range ids {
			if id == "" {
				return nil, invalid("Le paramètre theaters contient un identifiant de cinéma inconnu.")
			}
			position, ok := view.theaterByID[id]
			if !ok {
				return nil, invalid("Le paramètre theaters contient un identifiant de cinéma inconnu.")
			}
			if !seen[position] {
				seen[position] = true
				selected = append(selected, position)
			}
		}
		sort.Ints(selected)
		return selected, nil
	}
	requestedCity := strings.TrimSpace(city)
	if useDefault {
		return view.positionsForCities(s.cityLookupValues(s.options.DefaultCity)), nil
	}
	if requestedCity == "" {
		return append([]int(nil), view.theaterPositions...), nil
	}
	return view.positionsForCities(s.cityLookupValues(requestedCity)), nil
}

func (s *Service) cityLookupValues(requested string) []string {
	values := []string{requested}
	for alias, cities := range s.options.CityAliases {
		if !strings.EqualFold(alias, requested) {
			continue
		}
		for _, city := range cities {
			duplicate := false
			for _, value := range values {
				if strings.EqualFold(value, city) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				values = append(values, city)
			}
		}
	}
	return values
}

func (s *Service) parseDate(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, invalid("Le paramètre date est requis.")
	}
	parsed, err := time.ParseInLocation(dateLayout, value, s.location)
	if err != nil || parsed.Format(dateLayout) != value {
		return time.Time{}, invalid("Le paramètre date doit respecter le format YYYY-MM-DD.")
	}
	return parsed, nil
}

func (s *Service) parseServiceClock(date time.Time, value, parameter string) (time.Time, error) {
	if value == "" {
		return time.Time{}, invalid(fmt.Sprintf("Le paramètre %s est requis.", parameter))
	}
	if len(value) != 5 || value[2] != ':' {
		return time.Time{}, invalid(fmt.Sprintf("Le paramètre %s doit respecter le format HH:MM.", parameter))
	}
	hour, hourErr := strconv.Atoi(value[:2])
	minute, minuteErr := strconv.Atoi(value[3:])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return time.Time{}, invalid(fmt.Sprintf("Le paramètre %s doit respecter le format HH:MM.", parameter))
	}
	if hour > 2 && hour < 8 || hour == 2 && minute > 0 {
		return time.Time{}, invalid(fmt.Sprintf("Le paramètre %s doit appartenir à la journée cinéma (08:00–02:00).", parameter))
	}
	if hour < 8 {
		date = date.AddDate(0, 0, 1)
	}
	return localTime(date, hour, minute).UTC(), nil
}

func validateLanguage(language string) error {
	if language != LanguageAll && language != LanguageVOSTFR && language != LanguageVF {
		return invalid("Le paramètre language doit être ALL, VOSTFR ou VF.")
	}
	return nil
}

func matchesLanguage(session, requested string) bool {
	return requested == LanguageAll || requested == session || requested == LanguageVF && session == LanguageVFSME
}

func validateSlotFormat(format string) error {
	if format != "" && format != FormatAll && !validFormat(format) {
		return invalid("Le paramètre format doit être ALL, 2D, 3D, IMAX, DOLBY, SCREENX, LASER_ULTRA ou 4DX.")
	}
	return nil
}

func matchesFormat(session, requested string) bool {
	return requested == "" || requested == FormatAll || requested == session
}
