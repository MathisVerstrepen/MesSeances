package schedule

type theaterFixture struct {
	id             string
	slug           string
	name           string
	city           string
	acceptedPasses []string
}

type showtimeFixture struct {
	id             string
	theaterID      string
	movieSlug      string
	movieTitle     string
	startClock     string
	runtimeMinutes int
	language       string
	format         string
	room           string
}

var theaterFixtures = []theaterFixture{
	{
		id:             "ugc-lille",
		slug:           "ugc-lille",
		name:           "UGC Ciné Cité Lille",
		city:           "Lille",
		acceptedPasses: []string{"UGC_ILLIMITE"},
	},
	{
		id:             "ugc-villeneuve",
		slug:           "ugc-villeneuve",
		name:           "UGC Ciné Cité Villeneuve-d'Ascq",
		city:           "Lille",
		acceptedPasses: []string{"UGC_ILLIMITE"},
	},
}

var showtimeFixtures = []showtimeFixture{
	{
		id:             "seance-lumieres-lille-1200",
		theaterID:      "ugc-lille",
		movieSlug:      "echo-des-lumieres",
		movieTitle:     "L'Écho des lumières",
		startClock:     "12:00",
		runtimeMinutes: 100,
		language:       LanguageVOSTFR,
		format:         "2D",
		room:           "Salle 1",
	},
	{
		id:             "seance-ete-lille-1430",
		theaterID:      "ugc-lille",
		movieSlug:      "un-ete-a-lille",
		movieTitle:     "Un été à Lille",
		startClock:     "14:30",
		runtimeMinutes: 95,
		language:       LanguageVF,
		format:         "2D",
		room:           "Salle 4",
	},
	{
		id:             "seance-traversee-villeneuve-1230",
		theaterID:      "ugc-villeneuve",
		movieSlug:      "traversee-immobile",
		movieTitle:     "La Traversée immobile",
		startClock:     "12:30",
		runtimeMinutes: 115,
		language:       LanguageVOSTFR,
		format:         "2D",
		room:           "Salle 3",
	},
	{
		id:             "seance-minuit-villeneuve-0015",
		theaterID:      "ugc-villeneuve",
		movieSlug:      "minuit-grand-place",
		movieTitle:     "Minuit sur la Grand-Place",
		startClock:     "00:15",
		runtimeMinutes: 75,
		language:       LanguageVF,
		format:         "2D",
		room:           "Salle 6",
	},
}
