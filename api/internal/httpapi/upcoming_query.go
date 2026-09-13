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
		if key != "month" && key != "genres" && key != "page" && key != "page_size" || len(values) != 1 || values[0] == "" {
			return nil, fmt.Errorf("invalid upcoming query")
		}
	}
	return query, nil
}
