package enrichment

import (
	"strings"
	"testing"
)

func TestMegaramaSourceIdentities(t *testing.T) {
	for _, id := range []string{"ABCDE", "EMS0565-emsx0565HC1"} {
		if !validSourceIdentity(SourceMegarama, id) {
			t.Fatal("valid identity rejected")
		}
	}
	for _, id := range []string{"abcde", "emsx0565HC1", "EMS0565-emsx1315HC1", "EMS0565-emsx0565HC" + strings.Repeat("1", 115)} {
		if validSourceIdentity(SourceMegarama, id) {
			t.Fatal("unsafe identity accepted")
		}
	}
}
