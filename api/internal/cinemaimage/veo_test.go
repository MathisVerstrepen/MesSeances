package cinemaimage

import "testing"

func TestVeoPhotoIdentityAndInventoryFilter(t *testing.T) {
	if !(ListQuery{Limit: 20, Provider: "veo"}).Valid() {
		t.Fatal("veo filter rejected")
	}
	for _, id := range []string{"P1028", "EMS1361"} {
		if !validIdentity(Identity{Provider: "veo", ProviderTheaterID: id}) {
			t.Fatal(id)
		}
	}
	for _, id := range []string{"p1028", "EMS9999", "veo-P1028"} {
		if validIdentity(Identity{Provider: "veo", ProviderTheaterID: id}) {
			t.Fatal(id)
		}
	}
}
