package tfl

import (
	"context"
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"Query\":\"London Bridge\",\"Total\":1,\"Matches\":[{\"ID\":\"490000139R\",\"Name\":\"London Bridge Station\",\"Lat\":51.5,\"Lon\":-0.08,\"Modes\":[\"bus\"]}]}"))
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

	journey, err := c.Journey(context.Background(), "London Bridge", "Paddington", JourneyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(journey.Journeys) != 1 || journey.Journeys[0].Duration != 30 {
		t.Fatalf("unexpected journey: %+v", journey)
	}
}
