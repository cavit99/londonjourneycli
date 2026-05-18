package tfl

import (
	"context"
	"errors"
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
	if got := FilterArrivals([]Arrival{}, "", ""); got == nil || len(got) != 0 {
		t.Fatalf("expected non-nil empty arrivals, got %+v", got)
	}
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

func TestRequestErrorsPreserveCancellationCause(t *testing.T) {
	c := NewClient("SECRETKEY123")
	c.BaseURL = "http://127.0.0.1:1"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Arrivals(ctx, "490000139R")
	if err == nil {
		t.Fatal("expected request error")
	}
	msg := err.Error()
	if strings.Contains(msg, "SECRETKEY123") || strings.Contains(msg, "app_key") {
		t.Fatalf("request error leaked URL credentials: %s", msg)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected sanitized error to wrap context.Canceled, got %v", err)
	}
}

func TestLiveTfLStopSearchArrivalsJourney(t *testing.T) {
	if os.Getenv("LONDONJOURNEYCLI_LIVE_TFL") != "1" {
		t.Skip("set LONDONJOURNEYCLI_LIVE_TFL=1 to run live TfL smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	c := NewClient(os.Getenv("TFL_APP_KEY"))
	c.HTTPClient.Timeout = 30 * time.Second
	search, err := c.StopSearchWithOptions(ctx, "London Bridge", StopSearchOptions{Modes: []string{"tube", "bus"}, MaxResults: 3})
	if err != nil {
		t.Fatalf("stop search: %v", err)
	}
	if search.Total == 0 || len(search.Matches) == 0 {
		t.Fatalf("expected London Bridge stop matches, got %+v", search)
	}
	stop, err := c.StopPoint(ctx, "490G00008459")
	if err != nil {
		t.Fatalf("stop point: %v", err)
	}
	if len(stop.Children) == 0 {
		t.Fatalf("expected child stops for parent stop, got %+v", stop)
	}
	if arrivals, err := c.LineArrivals(ctx, "490G00008459", []string{"N3"}, "", ""); err != nil {
		t.Fatalf("line arrivals endpoint should respond even when no vehicles are due: %v", err)
	} else if arrivals == nil {
		t.Fatal("expected non-nil arrivals slice")
	}
	journey, err := c.Journey(ctx, "London Bridge", "Paddington", JourneyOptions{
		Preference:              "LeastWalking",
		MaxWalkingMinutes:       "20",
		UseRealTimeLiveArrivals: true,
	})
	if err != nil {
		t.Fatalf("journey: %v", err)
	}
	if len(journey.Journeys) == 0 {
		t.Fatalf("expected at least one journey option")
	}
}
