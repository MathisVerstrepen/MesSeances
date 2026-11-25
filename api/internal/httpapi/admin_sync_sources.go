package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncsource"
)

// SyncSourceController lists and toggles the cinemas configured for generic
// platform connectors.
type SyncSourceController interface {
	List(context.Context) ([]syncsource.AdminSource, error)
	SetEnabled(context.Context, schedule.Provider, string, bool) (syncsource.AdminSource, error)
	SetAllEnabled(context.Context, schedule.Provider, bool) (int64, error)
}

var syncSourceKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type syncSourceItem struct {
	Provider  schedule.Provider `json:"provider"`
	Key       string            `json:"key"`
	Name      string            `json:"name"`
	Website   string            `json:"website,omitempty"`
	CNCID     string            `json:"cnc_id,omitempty"`
	Enabled   bool              `json:"enabled"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type syncSourcesResponse struct {
	Sources []syncSourceItem `json:"sources"`
}

type setSyncSourceRequest struct {
	Enabled *bool `json:"enabled"`
}

type setSyncSourcesRequest struct {
	Provider schedule.Provider `json:"provider"`
	Enabled  *bool             `json:"enabled"`
}

type setSyncSourcesResponse struct {
	Updated int64 `json:"updated"`
}

func toSyncSourceItem(source syncsource.AdminSource) syncSourceItem {
	return syncSourceItem{Provider: source.Provider, Key: source.Key, Name: source.Name, Website: source.Website, CNCID: source.CNCID, Enabled: source.Enabled, UpdatedAt: source.UpdatedAt.UTC()}
}

func (a *adminAPI) syncSources(w http.ResponseWriter, r *http.Request) {
	if a.syncSourceStore == nil {
		writeSyncSourceUnavailable(w)
		return
	}
	sources, err := a.syncSourceStore.List(r.Context())
	if err != nil {
		writeSyncSourceFailure(w)
		return
	}
	items := make([]syncSourceItem, len(sources))
	for i, source := range sources {
		items[i] = toSyncSourceItem(source)
	}
	writeJSON(w, http.StatusOK, syncSourcesResponse{Sources: items})
}

func (a *adminAPI) setSyncSource(w http.ResponseWriter, r *http.Request) {
	if a.syncSourceStore == nil {
		writeSyncSourceUnavailable(w)
		return
	}
	provider := schedule.Provider(chi.URLParam(r, "provider"))
	key := chi.URLParam(r, "key")
	var request setSyncSourceRequest
	if !schedule.IsSourceProvider(provider) || !syncSourceKey.MatchString(key) || decodeAdminJSON(w, r, &request) != nil || request.Enabled == nil {
		writeInvalidSyncSource(w)
		return
	}
	source, err := a.syncSourceStore.SetEnabled(r.Context(), provider, key, *request.Enabled)
	if errors.Is(err, syncsource.ErrNotFound) {
		writeError(w, http.StatusNotFound, "sync_source_not_found", "Cinéma source introuvable.")
		return
	}
	if err != nil {
		writeSyncSourceFailure(w)
		return
	}
	writeJSON(w, http.StatusOK, toSyncSourceItem(source))
}

// setSyncSources enables or disables every source of one provider, or of all
// providers when the provider is empty.
func (a *adminAPI) setSyncSources(w http.ResponseWriter, r *http.Request) {
	if a.syncSourceStore == nil {
		writeSyncSourceUnavailable(w)
		return
	}
	var request setSyncSourcesRequest
	if decodeAdminJSON(w, r, &request) != nil || request.Enabled == nil || request.Provider != "" && !schedule.IsSourceProvider(request.Provider) {
		writeInvalidSyncSource(w)
		return
	}
	updated, err := a.syncSourceStore.SetAllEnabled(r.Context(), request.Provider, *request.Enabled)
	if err != nil {
		writeSyncSourceFailure(w)
		return
	}
	writeJSON(w, http.StatusOK, setSyncSourcesResponse{Updated: updated})
}

func writeInvalidSyncSource(w http.ResponseWriter) {
	writeError(w, http.StatusBadRequest, "invalid_sync_source", "Requête de cinéma source invalide.")
}

func writeSyncSourceUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "sync_source_unavailable", "Gestion des cinémas sources indisponible.")
}

func writeSyncSourceFailure(w http.ResponseWriter) {
	writeError(w, http.StatusBadGateway, "sync_source_failed", "La mise à jour des cinémas sources a échoué.")
}
