package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
)

func parseUpcomingQuery(r *http.Request) (url.Values, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("invalid upcoming query")
	}
	for key, values := range query {
		if (key != "page" && key != "view" && key != "year" && key != "month") || len(values) != 1 || values[0] == "" {
			return nil, fmt.Errorf("invalid upcoming query")
		}
	}
	return query, nil
}
