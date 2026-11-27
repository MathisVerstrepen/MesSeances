package httpapi

import (
	"net/http"

	"messeances/api/internal/accounts"
)

type accountPushSubscription struct {
	ExpectedUsername *string `json:"expected_username"`
	Endpoint         *string `json:"endpoint"`
	P256DH           *string `json:"p256dh"`
	Auth             *string `json:"auth"`
	Label            *string `json:"label"`
}

type accountPushDelete struct {
	ExpectedUsername *string `json:"expected_username"`
	ID               *string `json:"id"`
	Endpoint         *string `json:"endpoint"`
}

type accountPushTest struct {
	ExpectedUsername *string `json:"expected_username"`
	ID               *string `json:"id"`
}

func (h *accountHTTP) pushDevices(w http.ResponseWriter, r *http.Request) {
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.PushDevices(r.Context(), raw)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) savePushSubscription(w http.ResponseWriter, r *http.Request) {
	var input accountPushSubscription
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.Endpoint == nil || input.P256DH == nil || input.Auth == nil || input.Label == nil {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	view, err := h.service.SavePushSubscription(r.Context(), raw, *input.ExpectedUsername, *input.Endpoint, *input.P256DH, *input.Auth, *input.Label)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) deletePushSubscription(w http.ResponseWriter, r *http.Request) {
	var input accountPushDelete
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || (input.ID == nil) == (input.Endpoint == nil) {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	id, endpoint := "", ""
	if input.ID != nil {
		id = *input.ID
		if id == "" {
			accountError(w, accounts.ErrInvalidInput)
			return
		}
	} else {
		endpoint = *input.Endpoint
		if endpoint == "" {
			accountError(w, accounts.ErrInvalidInput)
			return
		}
	}
	view, err := h.service.DeletePushSubscription(r.Context(), raw, *input.ExpectedUsername, id, endpoint)
	if err != nil {
		accountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *accountHTTP) testPushSubscription(w http.ResponseWriter, r *http.Request) {
	var input accountPushTest
	if !accountJSON(w, r, &input) {
		return
	}
	if input.ExpectedUsername == nil || input.ID == nil {
		accountError(w, accounts.ErrInvalidInput)
		return
	}
	raw, err := h.cookie(r)
	if err != nil {
		accountError(w, err)
		return
	}
	if err := h.service.TestPushSubscription(r.Context(), raw, *input.ExpectedUsername, *input.ID); err != nil {
		accountError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
