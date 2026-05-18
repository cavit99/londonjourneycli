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
	mux.HandleFunc("/Line/Mode/tube,dlr,elizabeth-line,overground,tram/Status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[{\"id\":\"victoria\",\"name\":\"Victoria\",\"modeName\":\"tube\",\"lineStatuses\":[{\"statusSeverity\":10,\"statusSeverityDescription\":\"Good Service\"}]}]"))
	})
	mux.HandleFunc("/Line/victoria/Status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[{\"id\":\"victoria\",\"name\":\"Victoria\",\"modeName\":\"tube\",\"lineStatuses\":[{\"statusSeverity\":6,\"statusSeverityDescription\":\"Severe Delays\",\"disruption\":{\"category\":\"RealTime\",\"type\":\"lineInfo\",\"description\":\"Minor platform crowding\",\"closureText\":\"minorDelays\"}}]}]"))
	})
	mux.HandleFunc("/Line/victoria/Route", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[{\"id\":\"victoria\",\"name\":\"Victoria\",\"modeName\":\"tube\",\"routeSections\":[{\"direction\":\"inbound\",\"originationName\":\"Walthamstow Central\",\"destinationName\":\"Brixton\",\"originator\":\"940GZZLUWWL\",\"destination\":\"940GZZLUBXN\",\"serviceType\":\"Regular\"}]}]"))
	})
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
	mux.HandleFunc("/StopPoint", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("modes"); got != "bus" {
			t.Fatalf("modes=%q", got)
		}
		if got := r.URL.Query().Get("stopTypes"); got != "NaptanPublicBusCoachTram" {
			t.Fatalf("stopTypes=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"stopPoints\":[{\"naptanId\":\"490FAR\",\"commonName\":\"Far Stop\",\"lat\":51.51,\"lon\":-0.09,\"distance\":100,\"modes\":[\"bus\"]},{\"id\":\"490000139R\",\"commonName\":\"London Bridge Bus Station\",\"indicator\":\"Stop D\",\"stopLetter\":\"D\",\"lat\":51.5,\"lon\":-0.08,\"distance\":42,\"modes\":[\"bus\"]}]}"))
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
	mux.HandleFunc("/StopPoint/490G00008459/Arrivals", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	mux.HandleFunc("/StopPoint/490008459S/Arrivals", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := "[{\"LineName\":\"N3\",\"DestinationName\":\"Bromley North\",\"StationName\":\"Ildersly Grove\",\"PlatformName\":\"WH\",\"Towards\":\"Crystal Palace\",\"ExpectedArrival\":\"2026-05-18T01:04:00Z\",\"TimeToStation\":240,\"VehicleID\":\"n3\"}]"
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/StopPoint/490GSKIP", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"id\":\"490GSKIP\",\"commonName\":\"Mixed Parent\",\"children\":[{\"id\":\"490BAD\",\"commonName\":\"Bad Child\",\"modes\":[\"bus\"]},{\"id\":\"490GOOD\",\"commonName\":\"Good Child\",\"modes\":[\"bus\"]}]}"))
	})
	mux.HandleFunc("/Line/N3/Arrivals/490GSKIP", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	mux.HandleFunc("/Line/N3/Arrivals/490BAD", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad child", http.StatusBadGateway)
	})
	mux.HandleFunc("/Line/N3/Arrivals/490GOOD", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := "[{\"LineName\":\"N3\",\"DestinationName\":\"Bromley North\",\"StationName\":\"Good Child\",\"PlatformName\":\"G\",\"Towards\":\"Crystal Palace\",\"ExpectedArrival\":\"2026-05-18T01:02:00Z\",\"TimeToStation\":120,\"VehicleID\":\"good\"}]"
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

	statuses, err := c.LineStatus(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].LineStatuses[0].StatusSeverityDescription != "Good Service" {
		t.Fatalf("unexpected statuses: %+v", statuses)
	}
	disruptions, err := c.LineDisruptions(context.Background(), []string{"victoria"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(disruptions) != 1 || disruptions[0].LineName != "Victoria" || disruptions[0].Description != "Minor platform crowding" {
		t.Fatalf("unexpected disruptions: %+v", disruptions)
	}
	routes, err := c.LineRoutes(context.Background(), []string{"victoria"})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || len(routes[0].RouteSections) != 1 || routes[0].RouteSections[0].DestinationName != "Brixton" {
		t.Fatalf("unexpected routes: %+v", routes)
	}

	search, err := c.StopSearch(context.Background(), "London Bridge")
	if err != nil {
		t.Fatal(err)
	}
	if search.Total != 1 || search.Matches[0].ID != "490000139R" {
		t.Fatalf("unexpected search: %+v", search)
	}
	nearby, err := c.NearbyStops(context.Background(), NearbyStopOptions{Lat: 51.5, Lon: -0.08, Radius: 500, Modes: []string{"bus"}, StopTypes: []string{"NaptanPublicBusCoachTram"}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(nearby) != 1 || nearby[0].ID != "490000139R" || nearby[0].Distance == nil || *nearby[0].Distance != 42 {
		t.Fatalf("unexpected nearby stops: %+v", nearby)
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
	plainFallbackArrivals, err := c.LineArrivals(context.Background(), "490G00008459", nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plainFallbackArrivals) != 1 || plainFallbackArrivals[0].PlatformName != "WH" {
		t.Fatalf("unexpected no-line child fallback arrivals: %+v", plainFallbackArrivals)
	}
	mixedFallbackArrivals, err := c.LineArrivals(context.Background(), "490GSKIP", []string{"N3"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(mixedFallbackArrivals) != 1 || mixedFallbackArrivals[0].PlatformName != "G" {
		t.Fatalf("expected good child arrival despite bad sibling, got %+v", mixedFallbackArrivals)
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
		_, _ = w.Write([]byte(`{"toLocationDisambiguation":{"matchStatus":"list","disambiguationOptions":[{"parameterValue":"1000109","uri":"/journey/journeyresults/1000139/to/1000109","place":{"commonName":"Highgate (London), Highgate","placeType":"StopPoint","naptanId":"490000109S","icsCode":"1000109","modes":["tube","bus"],"lat":51.5777,"lon":-0.1457},"matchQuality":1000}]},"fromLocationDisambiguation":{"matchStatus":"identified"},"journeyVector":{"from":"1000139","to":"Highgate"}}`))
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
