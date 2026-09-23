package main

import (
	"context"
	"testing"

	runtimeconfig "messeances/api/internal/config"
)

func TestDisabledAccountMailInitializesNothing(t *testing.T) {
	// Nil pool/logger and an uncanceled context are intentional: disabled startup
	// must return before constructing clients, resolving credentials or workers.
	runAccountMail(context.Background(), nil, runtimeconfig.AccountsConfig{}, nil)
}
