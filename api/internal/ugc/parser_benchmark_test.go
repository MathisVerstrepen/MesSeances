package ugc

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"messeances/api/internal/schedule"
)

var benchmarkShowtimes []schedule.ShowtimeRecord

func BenchmarkParseShowingsFixture(b *testing.B) {
	body, err := os.ReadFile("testdata/showings.html")
	if err != nil {
		b.Fatal(err)
	}
	benchmarkParseShowings(b, body, Cinema{ProviderID: "25"}, "2026-08-15", 2)
}

func BenchmarkParseShowingsScale(b *testing.B) {
	const films = 64
	const showingsPerFilm = 8
	body := generatedShowingsBenchmarkBody(films, showingsPerFilm)
	benchmarkParseShowings(b, body, Cinema{ProviderID: "25"}, "2026-08-15", films*showingsPerFilm)
}

func BenchmarkParseShowingsNextSessionScale(b *testing.B) {
	const films = 256
	var body strings.Builder
	for film := 1; film <= films; film++ {
		fmt.Fprintf(&body, `<article id="bloc-showing-film-%[1]d"><div class="block--title text-uppercase"><a class="color--dark-blue" href="film_public_%[1]d.html?cinemaId=25">Film %[1]d</a></div><p>Prochaine séance le dimanche 16 août 2026</p></article>`, film)
	}
	benchmarkParseShowings(b, []byte(body.String()), Cinema{ProviderID: "25"}, "2026-08-15", 0)
}

func benchmarkParseShowings(b *testing.B, body []byte, cinema Cinema, serviceDate string, want int) {
	b.Helper()
	records, err := ParseShowings(strings.NewReader(string(body)), cinema, serviceDate)
	if err != nil || len(records) != want {
		b.Fatalf("preflight records=%d want=%d error=%v", len(records), want, err)
	}
	bodyString := string(body)
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for range b.N {
		records, err = ParseShowings(strings.NewReader(bodyString), cinema, serviceDate)
		if err != nil || len(records) != want {
			b.Fatalf("records=%d want=%d error=%v", len(records), want, err)
		}
		benchmarkShowtimes = records
	}
}

func generatedShowingsBenchmarkBody(films, showingsPerFilm int) []byte {
	var body strings.Builder
	body.WriteString(`<section id="showings">`)
	showingID := 1
	for film := 1; film <= films; film++ {
		fmt.Fprintf(&body, `<article id="bloc-showing-film-%[1]d"><div class="block--title text-uppercase"><a class="color--dark-blue" href="film_public_%[1]d.html?cinemaId=25">Film public %[1]d</a></div><img src="https://example.test/poster.jpg"><span>(1h45)</span><div class="session"><span class="screening-room">Salle %[1]d</span><span class="screening-2D3D">3D</span>`, film)
		for showing := 0; showing < showingsPerFilm; showing++ {
			fmt.Fprintf(&body, `<button data-showing="%d" data-film="%d" data-cinema="25" data-version="VF" data-seancedate="15/08/2026" data-seancehour="%02d:%02d">Réserver <span class="screening-time-end">(fin 23:59)</span></button>`, showingID, film, 8+(showing%12), (showing*5)%60)
			showingID++
		}
		body.WriteString(`</div></article>`)
	}
	body.WriteString(`</section>`)
	return []byte(body.String())
}
