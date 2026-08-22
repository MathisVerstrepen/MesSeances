package ugc

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"messeances/api/internal/schedule"
)

func providerNumericID(node *html.Node, preferred []string, legacy string) (string, bool) {
	selected := ""
	for _, name := range preferred {
		if value := strings.TrimSpace(attr(node, name)); value != "" {
			if _, ok := positiveNumericID(value); !ok || selected != "" && selected != value {
				return "", false
			}
			selected = value
		}
	}
	if selected != "" {
		return selected, true
	}
	return positiveNumericID(strings.TrimSpace(attr(node, legacy)))
}
func positiveNumericID(value string) (string, bool) {
	number, err := strconv.ParseUint(value, 10, 64)
	return value, err == nil && number > 0
}
func normalizeLanguage(value string) (schedule.Language, error) {
	switch value {
	case "VOSTF", "VOSTFR":
		return schedule.LanguageVOSTFR, nil
	case "VF":
		return schedule.LanguageVF, nil
	case "VFSTF":
		return schedule.LanguageVFSME, nil
	case "VO":
		return schedule.LanguageVO, nil
	default:
		return "", fmt.Errorf("unknown showing version")
	}
}
func showingFormat(button *html.Node, cache *showingsParseCache) schedule.Format {
	for ancestor := button.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if node := cache.firstDescendantWithClass(ancestor, "screening-2D3D"); node != nil {
			value := strings.ToUpper(collapse(cache.text(node)))
			switch {
			case strings.Contains(value, "IMAX"):
				return "IMAX"
			case strings.Contains(value, "DOLBY"):
				return "DOLBY"
			case strings.Contains(value, "4DX"):
				return "4DX"
			case strings.Contains(value, "3D"):
				return "3D"
			case value == "" || strings.Contains(value, "2D"):
				return "2D"
			default:
				return schedule.Format(value)
			}
		}
		if hasClass(ancestor, "session") || cache.firstDescendantWithClass(ancestor, "screening-room") != nil {
			return "2D"
		}
		if isShowingFilmBlock(ancestor) {
			break
		}
	}
	return "2D"
}
