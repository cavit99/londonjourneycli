package tfl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientAgainstHTTPServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/StopPoint/Search", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("query"); got != "London Bridge" {
			t.Fatalf("query=%q", got)
		}
		if modes := r.URL.Query()["modes"]; len(modes) > 0 {
			t.Fatalf("default stop search should not force modes, got %v", modes)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"Query\":\"London Bridge\",\"Total\":1,\"Matches\":[{\"ID\":\"490000139R\",\"Name\":\"London Bridge Station\",\"Lat\":51.5,\"Lon\":-0.08,\"Modes\":[\"bus\"]}]}"))
	})
	mux.HandleFunc("/Line/43/Arrivals/490000139R", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("direction"); got != "inbound" {
			t.Fatalf("direction=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		body := "[{\"LineName\":\"43\",\"DestinationName\":\"Friern Barnet\",\"StationName\":\"London Bridge Bus Station\",\"PlatformName\":\"D\",\"Towards\":\"Old Street\",\"ExpectedArrival\":\"2026-05-18T01:01:00Z\",\"TimeToStation\":60,\"VehicleID\":\"b\"}]"
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/StopPoint/490G00008459", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"id\":\"490G00008459\",\"commonName\":\"Ildersly Grove\",\"children\":[{\"id\":\"490008459S\",\"commonName\":\"Ildersly Grove\",\"indicator\":\"Stop WH\",\"stopLetter\":\"WH\",\"lat\":51.43,\"lon\":-0.09,\"modes\":[\"bus\"]}]}"))
	})
	mux.HandleFunc("/Line/N3/Arrivals/490G00008459", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("null"))
	})
	mux.HandleFunc("/Line/N3/Arrivals/490008459S", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := "[{\"LineName\":\"N3\",\"DestinationName\":\"Bromley North\",\"StationName\":\"Ildersly Grove\",\"PlatformName\":\"WH\",\"Towards\":\"Crystal Palace\",\"ExpectedArrival\":\"2026-05-18T01:04:00Z\",\"TimeToStation\":240,\"VehicleID\":\"n3\"}]"
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/StopPoint/490GEMPTY", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"id\":\"490GEMPTY\",\"commonName\":\"Empty Parent\",\"children\":[{\"id\":\"490EMPTYA\",\"commonName\":\"Empty Child\",\"modes\":[\"bus\"]}]}"))
	})
	mux.HandleFunc("/Line/N3/Arrivals/490GEMPTY", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("null"))
	})
	mux.HandleFunc("/Line/N3/Arrivals/490EMPTYA", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("null"))
	})
	mux.HandleFunc("/StopPoint/490GHUB", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"490GHUB","commonName":"Nested Hub","children":[{"id":"490GGROUP","commonName":"Nested Group","modes":["bus"]}]}`))
	})
	mux.HandleFunc("/StopPoint/490GGROUP", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"490GGROUP","commonName":"Nested Group","children":[{"id":"490LEAF","commonName":"Leaf Stop","indicator":"Stop L","stopLetter":"L","modes":["bus"]}]}`))
	})
	mux.HandleFunc("/StopPoint/490LEAF", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"490LEAF","commonName":"Leaf Stop","children":[]}`))
	})
	mux.HandleFunc("/Line/149/Arrivals/490GHUB", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	mux.HandleFunc("/Line/149/Arrivals/490GGROUP", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := `[{"LineName":"149","DestinationName":"Edmonton Green","StationName":"Leaf Stop","PlatformName":"L","Towards":"Liverpool Street","ExpectedArrival":"2026-05-18T01:05:00Z","TimeToStation":300,"VehicleID":"149"}]`
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/Line/149/Arrivals/490LEAF", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := `[{"LineName":"149","DestinationName":"Edmonton Green","StationName":"Leaf Stop","PlatformName":"L","Towards":"Liverpool Street","ExpectedArrival":"2026-05-18T01:05:00Z","TimeToStation":300,"VehicleID":"149"}]`
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/StopPoint/490000139R/Arrivals", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := "[{\"LineName\":\"43\",\"DestinationName\":\"Friern Barnet\",\"StationName\":\"London Bridge Bus Station\",\"PlatformName\":\"D\",\"Towards\":\"Old Street\",\"ExpectedArrival\":\"2026-05-18T01:03:00Z\",\"TimeToStation\":180,\"VehicleID\":\"a\"},{\"LineName\":\"43\",\"DestinationName\":\"Friern Barnet\",\"StationName\":\"London Bridge Bus Station\",\"PlatformName\":\"D\",\"Towards\":\"Old Street\",\"ExpectedArrival\":\"2026-05-18T01:01:00Z\",\"TimeToStation\":60,\"VehicleID\":\"b\"}]"
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/Journey/JourneyResults/1000139/to/1000174", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "journeyPreference=LeastTime") {
			t.Fatalf("missing journey preference: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		body := "{\"Journeys\":[{\"StartDateTime\":\"2026-05-18T01:00:00\",\"ArrivalDateTime\":\"2026-05-18T01:30:00\",\"Duration\":30,\"Legs\":[{\"Mode\":{\"Name\":\"tube\"},\"DepartureTime\":\"2026-05-18T01:00:00\",\"ArrivalTime\":\"2026-05-18T01:30:00\",\"DeparturePoint\":{\"CommonName\":\"London Bridge\"},\"ArrivalPoint\":{\"CommonName\":\"Paddington\"},\"RouteOptions\":[{\"Name\":\"Jubilee\"}]}]}]}"
		_, _ = w.Write([]byte(body))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient("")
	c.BaseURL = srv.URL

	search, err := c.StopSearch(context.Background(), "London Bridge")
	if err != nil {
		t.Fatal(err)
	}
	if search.Total != 1 || search.Matches[0].ID != "490000139R" {
		t.Fatalf("unexpected search: %+v", search)
	}

	arrivals, err := c.Arrivals(context.Background(), "490000139R")
	if err != nil {
		t.Fatal(err)
	}
	if arrivals[0].VehicleID != "b" {
		t.Fatalf("arrivals not sorted: %+v", arrivals)
	}
	lineArrivals, err := c.LineArrivals(context.Background(), "490000139R", []string{"43"}, "inbound", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(lineArrivals) != 1 || lineArrivals[0].LineName != "43" {
		t.Fatalf("unexpected line arrivals: %+v", lineArrivals)
	}
	fallbackArrivals, err := c.LineArrivals(context.Background(), "490G00008459", []string{"N3"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(fallbackArrivals) != 1 || fallbackArrivals[0].PlatformName != "WH" {
		t.Fatalf("unexpected child fallback arrivals: %+v", fallbackArrivals)
	}
	emptyFallbackArrivals, err := c.LineArrivals(context.Background(), "490GEMPTY", []string{"N3"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if emptyFallbackArrivals == nil || len(emptyFallbackArrivals) != 0 {
		t.Fatalf("expected non-nil empty child fallback arrivals, got %+v", emptyFallbackArrivals)
	}
	nestedFallbackArrivals, err := c.LineArrivals(context.Background(), "490GHUB", []string{"149"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(nestedFallbackArrivals) != 1 || nestedFallbackArrivals[0].PlatformName != "L" {
		t.Fatalf("unexpected nested child fallback arrivals: %+v", nestedFallbackArrivals)
	}

	journey, err := c.Journey(context.Background(), "London Bridge", "Paddington", JourneyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(journey.Journeys) != 1 || journey.Journeys[0].Duration != 30 {
		t.Fatalf("unexpected journey: %+v", journey)
	}
}

func TestJourneyDisambiguationErrorIsStructured(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/Journey/JourneyResults/1000139/to/Highgate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMultipleChoices)
		_, _ = w.Write([]byte(`{"toLocationDisambiguation":{"matchStatus":"list","disambiguationOptions":[{"parameterValue":"1000109","uri":"/journey/journeyresults/se192xe/to/1000109","place":{"commonName":"Highgate (London), Highgate","placeType":"StopPoint","naptanId":"490000109S","icsCode":"1000109","modes":["tube","bus"],"lat":51.5777,"lon":-0.1457},"matchQuality":1000}]},"fromLocationDisambiguation":{"matchStatus":"identified"},"journeyVector":{"from":"SE192XE","to":"Highgate"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient("")
	c.BaseURL = srv.URL
	_, err := c.Journey(context.Background(), "London Bridge", "Highgate", JourneyOptions{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T %[1]v", err)
	}
	if apiErr.StatusCode != http.StatusMultipleChoices || apiErr.Disambiguation == nil {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
	if apiErr.Disambiguation.ToLocationDisambiguation == nil {
		t.Fatalf("missing to-location disambiguation: %+v", apiErr.Disambiguation)
	}
	options := apiErr.Disambiguation.ToLocationDisambiguation.DisambiguationOptions
	if len(options) != 1 || options[0].ParameterValue != "1000109" || options[0].Place.CommonName != "Highgate (London), Highgate" {
		t.Fatalf("unexpected disambiguation options: %+v", options)
	}
}

func TestJourneyOptionsAreSentToTfL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/Journey/JourneyResults/1000139/to/1000174", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for key, want := range map[string]string{
			"journeyPreference":        "LeastWalking",
			"via":                      "1000254",
			"mode":                     "tube,bus",
			"accessibilityPreference":  "NoEscalators,StepFreeToPlatform",
			"maxWalkingMinutes":        "15",
			"walkingSpeed":             "Fast",
			"includeAlternativeRoutes": "true",
			"useRealTimeLiveArrivals":  "true",
		} {
			if got := q.Get(key); got != want {
				t.Fatalf("%s=%q want %q in %s", key, got, want, r.URL.RawQuery)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"Journeys\":[]}"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient("")
	c.BaseURL = srv.URL
	_, err := c.Journey(context.Background(), "London Bridge", "Paddington", JourneyOptions{
		Via:                      "Waterloo",
		Preference:               "LeastWalking",
		Modes:                    []string{"tube,bus"},
		AccessibilityPreferences: []string{"NoEscalators", "StepFreeToPlatform"},
		MaxWalkingMinutes:        "15",
		WalkingSpeed:             "Fast",
		IncludeAlternativeRoutes: true,
		UseRealTimeLiveArrivals:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
}
