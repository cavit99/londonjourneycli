package tfl

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResolveKnownStop(t *testing.T) {
	if got := ResolveKnownStop(" London   Bridge "); got != "1000139" {
		t.Fatalf("expected London Bridge ICS, got %q", got)
	}
	if got := ResolveKnownStop("Somewhere Else"); got != "Somewhere Else" {
		t.Fatalf("unexpected resolution: %q", got)
	}
}

func TestFilterArrivals(t *testing.T) {
	arrivals := []Arrival{
		{LineName: "N3", DestinationName: "Bromley North", Towards: "Crystal Palace", TimeToStation: 120},
		{LineName: "12", DestinationName: "Dulwich Library", Towards: "Dulwich", TimeToStation: 60},
	}
	got := FilterArrivals(arrivals, "n3", "crystal")
	if len(got) != 1 || got[0].LineName != "N3" {
		t.Fatalf("unexpected filtered arrivals: %+v", got)
	}
}

func TestEndpointIncludesAppKey(t *testing.T) {
	c := NewClient("key")
	u, err := c.endpoint("/StopPoint/Search", map[string][]string{"query": {"London Bridge"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "app_key=key") {
		t.Fatalf("expected app_key in %s", u)
	}
}

func TestRequestErrorsDoNotLeakAppKey(t *testing.T) {
	c := NewClient("SECRETKEY123")
	c.BaseURL = "http://127.0.0.1:1"
	_, err := c.Arrivals(context.Background(), "490000139R")
	if err == nil {
		t.Fatal("expected request error")
	}
	msg := err.Error()
	if strings.Contains(msg, "SECRETKEY123") || strings.Contains(msg, "app_key") {
		t.Fatalf("request error leaked URL credentials: %s", msg)
	}
}

func TestLiveTfLStopSearchArrivalsJourney(t *testing.T) {
	if os.Getenv("LONDONJOURNEYCLI_LIVE_TFL") != "1" {
		t.Skip("set LONDONJOURNEYCLI_LIVE_TFL=1 to run live TfL smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := NewClient(os.Getenv("TFL_APP_KEY"))
	search, err := c.StopSearch(ctx, "London Bridge")
	if err != nil {
		t.Fatalf("stop search: %v", err)
	}
	if search.Total == 0 || len(search.Matches) == 0 {
		t.Fatalf("expected London Bridge stop matches, got %+v", search)
	}
	if _, err := c.Arrivals(ctx, search.Matches[0].ID); err != nil {
		t.Fatalf("arrivals endpoint should respond even when no vehicles are due: %v", err)
	}
	journey, err := c.Journey(ctx, "London Bridge", "Paddington", JourneyOptions{})
	if err != nil {
		t.Fatalf("journey: %v", err)
	}
	if len(journey.Journeys) == 0 {
		t.Fatalf("expected at least one journey option")
	}
}
