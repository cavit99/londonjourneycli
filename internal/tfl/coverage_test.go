package tfl

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAPIErrorAndParsingVariants(t *testing.T) {
	if got := ((*APIError)(nil)).Error(); got != "" {
		t.Fatalf("nil error=%q", got)
	}
	if got := (&APIError{StatusCode: http.StatusTooManyRequests}).Error(); got != "tfl http 429" {
		t.Fatalf("status error=%q", got)
	}
	if got := (&APIError{StatusCode: http.StatusBadRequest, Message: "bad place"}).Error(); got != "tfl http 400: bad place" {
		t.Fatalf("message error=%q", got)
	}

	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "message", status: http.StatusBadRequest, body: "{\"message\":\"bad request\"}", want: "bad request"},
		{name: "error", status: http.StatusInternalServerError, body: "{\"error\":\"upstream broke\"}", want: "upstream broke"},
		{name: "fallback", status: http.StatusTeapot, body: "not json", want: "I'm a teapot"},
		{name: "empty 300", status: http.StatusMultipleChoices, body: "{}", want: "Multiple Choices"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := parseAPIError(tc.status, []byte(tc.body))
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected APIError, got %T", err)
			}
			if apiErr.Message != tc.want {
				t.Fatalf("message=%q want %q", apiErr.Message, tc.want)
			}
		})
	}
	disambigBody := "{\"fromLocationDisambiguation\":{\"disambiguationOptions\":[{\"parameterValue\":\"1000139\"}]}}"
	err := parseAPIError(http.StatusMultipleChoices, []byte(disambigBody))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Disambiguation == nil || apiErr.Message != "disambiguation required" {
		t.Fatalf("disambiguation not parsed: %#v", err)
	}
}

func TestStopPointTreeAndCSVHelpers(t *testing.T) {
	tree := []StopPoint{{ID: "parent", Children: []StopPoint{{ID: "child", Children: []StopPoint{{ID: "leaf"}}}}}}
	if got := findStopPointChild(tree, "leaf"); got == nil || got.ID != "leaf" {
		t.Fatalf("nested child not found: %+v", got)
	}
	if got := findStopPointChild(tree, "missing"); got != nil {
		t.Fatalf("unexpected child: %+v", got)
	}
	if !isLikelyGroupedStopID("490G123") || !isLikelyGroupedStopID("HUBXYZ") || !isLikelyGroupedStopID("910G1") || !isLikelyGroupedStopID("940G1") {
		t.Fatal("expected grouped stop IDs")
	}
	if isLikelyGroupedStopID("490012345") {
		t.Fatal("unexpected grouped stop ID")
	}
	if got := cleanCSV([]string{" bus, tube ", "", "dlr"}); strings.Join(got, "|") != "bus|tube|dlr" {
		t.Fatalf("cleanCSV=%v", got)
	}
	values := url.Values{}
	setCSVParam(values, "mode", []string{" bus ", "tube,dlr"})
	setBoolParam(values, "realTime", true)
	setBoolParam(values, "unused", false)
	if values.Get("mode") != "bus,tube,dlr" || values.Get("realTime") != "true" || values.Get("unused") != "" {
		t.Fatalf("values=%v", values)
	}
}

func TestArrivalDedupeKeysAndEndpointErrors(t *testing.T) {
	when := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	arrivals := []Arrival{
		{VehicleID: "v1", LineName: "43", DestinationName: "A", StationName: "S", PlatformName: "D", ExpectedArrival: when, TimeToStation: 120},
		{VehicleID: "v1", LineName: "43", DestinationName: "A", StationName: "S", PlatformName: "D", ExpectedArrival: when, TimeToStation: 120},
		{VehicleID: "v1", LineName: "43", DestinationName: "B", StationName: "S", PlatformName: "D", ExpectedArrival: when, TimeToStation: 180},
	}
	got := dedupeArrivals(arrivals)
	if len(got) != 2 {
		t.Fatalf("deduped arrivals=%+v", got)
	}
	if got := dedupeArrivals(arrivals[:1]); len(got) != 1 {
		t.Fatalf("single arrival changed: %+v", got)
	}
	c := NewClient("")
	c.BaseURL = "://bad-url"
	if _, err := c.endpoint("/StopPoint/Search", nil); err == nil {
		t.Fatal("expected invalid base URL error")
	}
}
