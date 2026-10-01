package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"messeances/api/internal/accounts"
)

type AdminAccountsLister interface {
	List(context.Context, accounts.AdminAccountsQuery) (accounts.AdminAccountsPage, error)
}

func adminAccountsPrivacy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		next.ServeHTTP(w, r)
	})
}

func (a *adminAPI) adminAccounts(w http.ResponseWriter, r *http.Request) {
	query, ok := parseAdminAccountsQuery(r.URL.RawQuery)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_query", "Pagination des comptes invalide.")
		return
	}
	if a.accounts == nil {
		writeError(w, http.StatusServiceUnavailable, "admin_accounts_unavailable", "Les comptes sont indisponibles.")
		return
	}
	page, err := a.accounts.List(r.Context(), query)
	if err != nil {
		if errors.Is(err, accounts.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "invalid_query", "Pagination des comptes invalide.")
			return
		}
		writeError(w, http.StatusServiceUnavailable, "admin_accounts_unavailable", "Les comptes sont indisponibles.")
		return
	}
	if page.Items == nil {
		page.Items = []accounts.AdminAccount{}
	}
	writeJSON(w, http.StatusOK, page)
}

func parseAdminAccountsQuery(rawQuery string) (accounts.AdminAccountsQuery, bool) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return accounts.AdminAccountsQuery{}, false
	}
	query := accounts.AdminAccountsQuery{Limit: 50}
	for key, entries := range values {
		if (key != "limit" && key != "offset") || len(entries) != 1 {
			return accounts.AdminAccountsQuery{}, false
		}
		value, err := strconv.ParseInt(entries[0], 10, 32)
		if err != nil {
			return accounts.AdminAccountsQuery{}, false
		}
		if key == "limit" {
			query.Limit = int(value)
		} else {
			query.Offset = int(value)
		}
	}
	return query, query.Valid()
}
