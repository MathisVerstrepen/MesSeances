package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"messeances/api/internal/cinemaimage"
	"messeances/api/internal/schedule"
)

type publicTheaterImagesFake struct {
	metadata                 func(context.Context, cinemaimage.Identity) (*cinemaimage.PublicImage, error)
	read                     func(context.Context, cinemaimage.Identity, int64) ([]byte, error)
	metadataCalls, readCalls int
}

func (f *publicTheaterImagesFake) PublicImage(ctx context.Context, id cinemaimage.Identity) (*cinemaimage.PublicImage, error) {
	f.metadataCalls++
	if f.metadata != nil {
		return f.metadata(ctx, id)
	}
	return nil, nil
}

func (f *publicTheaterImagesFake) Read(ctx context.Context, id cinemaimage.Identity, revision int64) ([]byte, error) {
	f.readCalls++
	if f.read != nil {
		return f.read(ctx, id, revision)
	}
	return nil, cinemaimage.ErrImageNotFound
}

func assertPublicCinemaHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
		t.Fatal("cache/cookie contract", response.Header())
	}
}

func assertCinemaDeadline(t *testing.T, ctx context.Context, bound time.Duration) {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > bound {
		t.Fatal("missing or excessive deadline", deadline, bound)
	}
}

func TestTheaterShowtimesCinemaImageMetadata(t *testing.T) {
	want := &cinemaimage.PublicImage{URL: "/api/v1/theaters/ugc/25/image/1", Width: 20, Height: 10}
	for _, name := range []string{"image", "nil controller", "disabled service", "no image", "storage error", "timeout"} {
		t.Run(name, func(t *testing.T) {
			f := &publicTheaterImagesFake{}
			f.metadata = func(ctx context.Context, id cinemaimage.Identity) (*cinemaimage.PublicImage, error) {
				assertCinemaDeadline(t, ctx, 500*time.Millisecond)
				if id != (cinemaimage.Identity{Provider: "ugc", ProviderTheaterID: "25"}) {
					t.Fatal("slug used as identity", id)
				}
				switch name {
				case "storage error":
					return nil, errors.New("private filesystem path")
				case "timeout":
					<-ctx.Done()
					return want, nil
				case "no image":
					return nil, nil
				default:
					return want, nil
				}
			}
			var controller PublicTheaterImageController = f
			if name == "nil controller" {
				controller = nil
			}
			if name == "disabled service" {
				controller = cinemaimage.NewService(nil, nil, nil, nil)
			}
			handler := testHandlerWithOptions(t, HandlerOptions{PublicTheaterImages: controller})
			response := performRequest(t, handler, "/api/v1/theaters/ugc-lille/showtimes")
			assertPublicCinemaHeaders(t, response)
			var payload map[string]any
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &payload) != nil {
				t.Fatal(response.Code, response.Body.String())
			}
			if !reflect.DeepEqual(sortedKeys(payload), []string{"date", "generated_at", "showtimes", "theater", "timezone"}) || payload["generated_at"] != "2026-08-14T12:00:00Z" || len(payload["showtimes"].([]any)) != 1 {
				t.Fatal(payload)
			}
			theater := payload["theater"].(map[string]any)
			if !reflect.DeepEqual(sortedKeys(theater), []string{"accepted_passes", "address", "available_dates", "city", "city_slug", "id", "image", "name", "postal_code", "provider", "slug"}) {
				t.Fatal(theater)
			}
			if name == "image" {
				image := theater["image"].(map[string]any)
				if !reflect.DeepEqual(image, map[string]any{"url": want.URL, "width": float64(20), "height": float64(10)}) {
					t.Fatal(image)
				}
			} else if theater["image"] != nil {
				t.Fatal("photo error affected null contract", theater)
			}
			calls := 1
			if name == "nil controller" || name == "disabled service" {
				calls = 0
			}
			if f.metadataCalls != calls || f.readCalls != 0 {
				t.Fatal("unexpected photo lookups", f.metadataCalls, f.readCalls)
			}
		})
	}

	t.Run("fresh metadata on same snapshot", func(t *testing.T) {
		var image *cinemaimage.PublicImage
		f := &publicTheaterImagesFake{metadata: func(context.Context, cinemaimage.Identity) (*cinemaimage.PublicImage, error) { return image, nil }}
		handler := testHandlerWithOptions(t, HandlerOptions{PublicTheaterImages: f})
		var first map[string]any
		for _, revision := range []int{0, 1, 2, 0} {
			image = nil
			if revision != 0 {
				image = &cinemaimage.PublicImage{URL: "/api/v1/theaters/ugc/25/image/" + strconv.Itoa(revision), Width: 20, Height: 10}
			}
			response := performRequest(t, handler, "/api/v1/theaters/ugc-lille/showtimes")
			assertPublicCinemaHeaders(t, response)
			var payload map[string]any
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &payload) != nil {
				t.Fatal(response.Code)
			}
			theater := payload["theater"].(map[string]any)
			if image == nil {
				if theater["image"] != nil {
					t.Fatal(theater)
				}
			} else if theater["image"].(map[string]any)["url"] != image.URL {
				t.Fatal(theater)
			}
			delete(theater, "image")
			if first == nil {
				first = payload
			} else if !reflect.DeepEqual(first, payload) {
				t.Fatal("schedule changed with photo", payload, first)
			}
		}
		if f.metadataCalls != 4 || f.readCalls != 0 {
			t.Fatal(f)
		}
		for _, path := range []string{"/api/v1/theaters/ugc-lille/showtimes?date=invalid", "/api/v1/theaters/unknown/showtimes"} {
			response := performRequest(t, handler, path)
			assertPublicCinemaHeaders(t, response)
			if response.Code != 400 && response.Code != 404 {
				t.Fatal(response.Code)
			}
		}
		for _, path := range []string{"/api/v1/theaters", "/api/v1/timeline?date=2026-08-15", "/api/v1/cities/lille"} {
			response := performRequest(t, handler, path)
			if response.Code != 200 || strings.Contains(response.Body.String(), `"image":`) {
				t.Fatal("shared DTO enriched", path, response.Body.String())
			}
		}
		if f.metadataCalls != 4 {
			t.Fatal("lookup for schedule error/list", f.metadataCalls)
		}
	})

	t.Run("no snapshot", func(t *testing.T) {
		f := &publicTheaterImagesFake{}
		handler := NewHandlerWithOptions(nil, "http://localhost:3000", HandlerOptions{PublicTheaterImages: f})
		response := performRequest(t, handler, "/api/v1/theaters/ugc-lille/showtimes")
		assertPublicCinemaHeaders(t, response)
		if response.Code != 503 || f.metadataCalls != 0 {
			t.Fatal(response.Code, f.metadataCalls)
		}
	})
}

func TestTheaterShowtimesCinemaImageProviderIdentity(t *testing.T) {
	for _, tc := range []struct {
		provider schedule.Provider
		id, want string
	}{
		{schedule.ProviderKinepolis, "kinepolis-001-opaque-ID", "001-opaque-ID"},
		{schedule.ProviderPathe, "pathe-001-opaque-ID", "001-opaque-ID"},
		{schedule.ProviderCineville, "cineville-1", "1"},
		{schedule.ProviderUGC, "ugc-25", "25"},
		{schedule.ProviderUGC, "25", ""},
		{schedule.ProviderUGC, "ugc-../25", ""},
	} {
		t.Run(tc.id, func(t *testing.T) {
			data := fixtureDataset(t)
			data.Provider = schedule.ProviderCombined
			data.Theaters = []schedule.TheaterRecord{{Provider: tc.provider, ID: tc.id, Slug: "fixture-slug", Name: "Fixture", City: "Lille", AvailableDates: []string{}}}
			data.Showtimes = nil
			service, err := schedule.NewService(fixtureSource{view: schedule.NewSnapshotView(data)}, schedule.ServiceOptions{})
			if err != nil {
				t.Fatal(err)
			}
			f := &publicTheaterImagesFake{metadata: func(_ context.Context, id cinemaimage.Identity) (*cinemaimage.PublicImage, error) {
				if id != (cinemaimage.Identity{Provider: string(tc.provider), ProviderTheaterID: tc.want}) {
					t.Fatal(id)
				}
				return nil, nil
			}}
			handler := NewHandlerWithOptions(service, "http://localhost:3000", HandlerOptions{PublicTheaterImages: f})
			response := performRequest(t, handler, "/api/v1/theaters/fixture-slug/showtimes")
			wantCalls := 1
			if tc.want == "" {
				wantCalls = 0
			}
			if response.Code != 200 || f.metadataCalls != wantCalls || !strings.Contains(response.Body.String(), `"image":null`) {
				t.Fatal(response.Code, response.Body.String(), f.metadataCalls)
			}
		})
	}
}

func TestPublicTheaterImageRead(t *testing.T) {
	bytes := []byte("normalized-webp-fixture")
	for _, tc := range []struct {
		name, path string
		err        error
		status     int
		absent     bool
	}{
		{"success", "/ugc/25/image/1", nil, 200, false},
		{"hyphenated", "/kinepolis/001-opaque-ID/image/9007199254740991", nil, 200, false},
		{"other provider", "/pathe/001-opaque-ID/image/1", nil, 200, false},
		{"zero", "/ugc/25/image/0", nil, 404, false},
		{"leading zero", "/ugc/25/image/01", nil, 404, false},
		{"negative", "/ugc/25/image/-1", nil, 404, false},
		{"overflow", "/ugc/25/image/9007199254740992", nil, 404, false},
		{"unknown provider", "/unknown/25/image/1", nil, 404, false},
		{"invalid identity", "/ugc/not-a-number/image/1", nil, 404, false},
		{"encoded separator", "/kinepolis/a%2Fb/image/1", nil, 404, false},
		{"encoded traversal", "/kinepolis/%2E%2E/image/1", nil, 404, false},
		{"disabled", "/ugc/25/image/1", nil, 404, true},
		{"nonmember", "/ugc/25/image/1", cinemaimage.ErrNotFound, 404, false},
		{"obsolete or absent file", "/ugc/25/image/1", cinemaimage.ErrImageNotFound, 404, false},
		{"bad request", "/ugc/25/image/1", cinemaimage.ErrRequest, 404, false},
		{"storage", "/ugc/25/image/1", cinemaimage.ErrStorage, 503, false},
		{"private error", "/ugc/25/image/1", errors.New("private path and credentials"), 503, false},
		{"timeout", "/ugc/25/image/1", context.DeadlineExceeded, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &publicTheaterImagesFake{read: func(ctx context.Context, id cinemaimage.Identity, revision int64) ([]byte, error) {
				assertCinemaDeadline(t, ctx, 2*time.Second)
				if "/"+id.Provider+"/"+id.ProviderTheaterID+"/image/"+strconv.FormatInt(revision, 10) != tc.path {
					t.Fatal("identity changed", id, revision)
				}
				return bytes, tc.err
			}}
			var controller PublicTheaterImageController = f
			if tc.absent {
				controller = nil
			}
			handler := NewHandlerWithOptions(nil, "http://localhost:3000", HandlerOptions{PublicTheaterImages: controller})
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/theaters"+tc.path, nil)
			request.Header.Set("Origin", "https://other.example")
			request.Header.Set("Range", "bytes=0-1")
			request.Header.Set("If-None-Match", "*")
			request.Header.Set("If-Modified-Since", time.Now().Format(http.TimeFormat))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertPublicCinemaHeaders(t, response)
			switch tc.status {
			case 200:
				if response.Code != 200 || !reflect.DeepEqual(response.Body.Bytes(), bytes) {
					t.Fatal(response.Code, response.Body.String())
				}
				for key, want := range map[string]string{"Content-Type": "image/webp", "Content-Length": strconv.Itoa(len(bytes)), "Content-Disposition": `inline; filename="cinema.webp"`, "X-Content-Type-Options": "nosniff", "Cross-Origin-Resource-Policy": "cross-origin"} {
					if response.Header().Get(key) != want {
						t.Fatal(key, response.Header())
					}
				}
			case 404:
				assertAPIError(t, response, 404, "cinema_image_not_found", "Image introuvable.")
			default:
				assertAPIError(t, response, 503, "cinema_images_unavailable", "Images des cinémas indisponibles.")
			}
			if f.metadataCalls != 0 {
				t.Fatal("read queried metadata wrapper")
			}
			wantCalls := 1
			if tc.err == nil && tc.status == 404 {
				wantCalls = 0
			}
			if f.readCalls != wantCalls {
				t.Fatal(f.readCalls, wantCalls)
			}
		})
	}
}

func TestPublicTheaterImageReadTimeoutAndRequestContext(t *testing.T) {
	f := &publicTheaterImagesFake{read: func(ctx context.Context, _ cinemaimage.Identity, _ int64) ([]byte, error) {
		assertCinemaDeadline(t, ctx, 2*time.Second)
		<-ctx.Done()
		return []byte("late bytes"), nil
	}}
	handler := NewHandlerWithOptions(nil, "http://localhost:3000", HandlerOptions{PublicTheaterImages: f})
	response := performRequest(t, handler, "/api/v1/theaters/ugc/25/image/1")
	assertPublicCinemaHeaders(t, response)
	assertAPIError(t, response, 503, "cinema_images_unavailable", "Images des cinémas indisponibles.")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/theaters/ugc/25/image/1", nil))
	assertAPIError(t, response, 503, "cinema_images_unavailable", "Images des cinémas indisponibles.")
}

func TestPublicTheaterImageRateLimit(t *testing.T) {
	for _, path := range []string{"/api/v1/theaters/ugc/25/image/1", "/api/v1/theaters/ugc-lille/showtimes"} {
		t.Run(path, func(t *testing.T) {
			f := &publicTheaterImagesFake{}
			handler := testHandlerWithOptions(t, HandlerOptions{PublicTheaterImages: f, RateLimitClock: func() time.Time { return time.Unix(0, 0) }})
			for range expensiveReadBurst {
				performRequest(t, handler, path)
			}
			response := performRequest(t, handler, path)
			assertPublicCinemaHeaders(t, response)
			if response.Code != 429 || f.readCalls+f.metadataCalls != expensiveReadBurst {
				t.Fatal(response.Code, f)
			}
		})
	}
}
