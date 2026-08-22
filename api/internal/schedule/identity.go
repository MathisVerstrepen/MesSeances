package schedule

import "fmt"

type MovieIdentity struct {
	Provider   Provider
	ProviderID string
}

func (i MovieIdentity) Validate() error {
	if !validProvider(i.Provider, false) || !validProviderIdentity(i.Provider, "movie", i.ProviderID) {
		return fmt.Errorf("invalid movie identity")
	}
	return nil
}
