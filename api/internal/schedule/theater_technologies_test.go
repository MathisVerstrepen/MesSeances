package schedule

import (
	"reflect"
	"testing"
)

func TestTheaterTechnologiesSnapshotPrecomputedAndDetached(t *testing.T) {
	data := testDataset()
	data.Showtimes[0].Format = FormatIMAX
	data.Theaters[0].Technologies = []Format{FormatICE, FormatIMAX}
	reader := &fakeSnapshotReader{version: 1, data: data}
	source, err := NewPostgresSource(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(source, ServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	oldView := source.Snapshot()
	data.Theaters[0].Technologies[0] = FormatDolby
	for range 3 {
		detail, err := service.TheaterShowtimes(TheaterShowtimesQuery{Slug: "ugc-25"})
		if err != nil || !reflect.DeepEqual(detail.Technologies, []Format{FormatICE, FormatIMAX}) || !reflect.DeepEqual(detail.AvailableFormats, []Format{Format2D, FormatIMAX}) {
			t.Fatalf("detail=%+v err=%v", detail, err)
		}
		catalog := service.Theaters(TheaterCatalogQuery{})
		for _, row := range catalog {
			if row.ID == "ugc-25" {
				if !reflect.DeepEqual(row.Technologies, detail.Technologies) {
					t.Fatal("directory/detail technologies diverged")
				}
				row.Technologies[0] = FormatScreenX
			}
		}
		detail.Technologies[0], detail.AvailableFormats[0] = FormatScreenX, FormatScreenX
	}
	if reader.loads != 1 || reader.checks != 0 {
		t.Fatal("request performed snapshot IO")
	}
	reader.data.Theaters[0].Technologies = []Format{FormatLaserUltra}
	reader.technologyVersion = 1
	if err := source.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if source.Snapshot() == oldView || reader.loads != 2 {
		t.Fatal("technology-only revision did not refresh")
	}
	detail, err := service.TheaterShowtimes(TheaterShowtimesQuery{Slug: "ugc-25"})
	if err != nil || !reflect.DeepEqual(detail.Technologies, []Format{FormatIMAX, FormatLaserUltra}) {
		t.Fatalf("refreshed=%+v err=%v", detail.Technologies, err)
	}
	if !reflect.DeepEqual(oldView.theaterTechnologies[0], []Format{FormatICE, FormatIMAX}) {
		t.Fatal("old snapshot changed")
	}
}
