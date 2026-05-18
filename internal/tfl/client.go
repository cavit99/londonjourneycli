package tfl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.tfl.gov.uk"

type Client struct {
	BaseURL    string
	AppKey     string
	HTTPClient *http.Client
}

func NewClient(appKey string) *Client {
	return &Client{BaseURL: DefaultBaseURL, AppKey: appKey, HTTPClient: &http.Client{Timeout: 15 * time.Second}}
}

type SearchResponse struct {
	Query   string        `json:"query"`
	Total   int           `json:"total"`
	Matches []MatchedStop `json:"matches"`
}

type MatchedStop struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Lat      float64  `json:"lat"`
	Lon      float64  `json:"lon"`
	Modes    []string `json:"modes"`
	ParentID string   `json:"topMostParentId,omitempty"`
}

type Arrival struct {
	LineName        string    `json:"lineName"`
	DestinationName string    `json:"destinationName"`
	StationName     string    `json:"stationName"`
	PlatformName    string    `json:"platformName"`
	Towards         string    `json:"towards"`
	ExpectedArrival time.Time `json:"expectedArrival"`
	TimeToStation   int       `json:"timeToStation"`
	VehicleID       string    `json:"vehicleId,omitempty"`
}

type rawArrival struct {
	LineName        string `json:"lineName"`
	DestinationName string `json:"destinationName"`
	StationName     string `json:"stationName"`
	PlatformName    string `json:"platformName"`
	Towards         string `json:"towards"`
	ExpectedArrival string `json:"expectedArrival"`
	TimeToStation   int    `json:"timeToStation"`
	VehicleID       string `json:"vehicleId"`
}

type JourneyResponse struct {
	Journeys []Journey `json:"journeys"`
}

type Journey struct {
	StartDateTime   string `json:"startDateTime"`
	ArrivalDateTime string `json:"arrivalDateTime"`
	Duration        int    `json:"duration"`
	Legs            []Leg  `json:"legs"`
}

type Leg struct {
	Mode           Mode          `json:"mode"`
	DepartureTime  string        `json:"departureTime"`
	ArrivalTime    string        `json:"arrivalTime"`
	DeparturePoint JourneyPoint  `json:"departurePoint"`
	ArrivalPoint   JourneyPoint  `json:"arrivalPoint"`
	RouteOptions   []RouteOption `json:"routeOptions"`
	Instruction    Instruction   `json:"instruction"`
	Duration       int           `json:"duration"`
}

type Mode struct {
	Name string `json:"name"`
}

type JourneyPoint struct {
	CommonName       string `json:"commonName"`
	IndividualStopID string `json:"individualStopId"`
	PlatformName     string `json:"platformName"`
}

type RouteOption struct {
	Name string `json:"name"`
}

type Instruction struct {
	Summary  string `json:"summary"`
	Detailed string `json:"detailed"`
}

func (c *Client) StopSearch(ctx context.Context, query string) (SearchResponse, error) {
	var out SearchResponse
	u, err := c.endpoint("/StopPoint/Search", url.Values{"query": {query}, "modes": {"bus"}})
	if err != nil {
		return out, err
	}
	err = c.getJSON(ctx, u, &out)
	return out, err
}

func (c *Client) Arrivals(ctx context.Context, stopID string) ([]Arrival, error) {
	u, err := c.endpoint("/StopPoint/"+url.PathEscape(stopID)+"/Arrivals", nil)
	if err != nil {
		return nil, err
	}
	var raw []rawArrival
	if err := c.getJSON(ctx, u, &raw); err != nil {
		return nil, err
	}
	arrivals := make([]Arrival, 0, len(raw))
	for _, r := range raw {
		t, err := time.Parse(time.RFC3339, r.ExpectedArrival)
		if err != nil {
			return nil, fmt.Errorf("parse arrival time %q: %w", r.ExpectedArrival, err)
		}
		arrivals = append(arrivals, Arrival{LineName: r.LineName, DestinationName: r.DestinationName, StationName: r.StationName, PlatformName: r.PlatformName, Towards: r.Towards, ExpectedArrival: t, TimeToStation: r.TimeToStation, VehicleID: r.VehicleID})
	}
	sort.Slice(arrivals, func(i, j int) bool { return arrivals[i].TimeToStation < arrivals[j].TimeToStation })
	return arrivals, nil
}

type JourneyOptions struct {
	Date     string
	Time     string
	Arriving bool
}

func (c *Client) Journey(ctx context.Context, from, to string, opts JourneyOptions) (JourneyResponse, error) {
	var out JourneyResponse
	from = ResolveKnownStop(from)
	to = ResolveKnownStop(to)
	params := url.Values{"nationalSearch": {"true"}, "journeyPreference": {"LeastTime"}}
	if opts.Date != "" {
		params.Set("date", opts.Date)
	}
	if opts.Time != "" {
		params.Set("time", opts.Time)
	}
	if opts.Arriving {
		params.Set("timeIs", "Arriving")
	} else {
		params.Set("timeIs", "Departing")
	}
	path := "/Journey/JourneyResults/" + url.PathEscape(from) + "/to/" + url.PathEscape(to)
	u, err := c.endpoint(path, params)
	if err != nil {
		return out, err
	}
	err = c.getJSON(ctx, u, &out)
	return out, err
}

func FilterArrivals(arrivals []Arrival, line, towards string) []Arrival {
	line = strings.ToLower(strings.TrimSpace(line))
	towards = strings.ToLower(strings.TrimSpace(towards))
	var out []Arrival
	for _, a := range arrivals {
		if line != "" && strings.ToLower(a.LineName) != line {
			continue
		}
		if towards != "" {
			hay := strings.ToLower(a.Towards + " " + a.DestinationName)
			if !strings.Contains(hay, towards) {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

func (c *Client) endpoint(path string, values url.Values) (string, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	u, err := url.Parse(base + path)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for k, vs := range values {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	if c.AppKey != "" {
		q.Set("app_key", c.AppKey)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (c *Client) getJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "londonjourneycli/0.1")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return sanitizeRequestError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tfl http %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func sanitizeRequestError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	return fmt.Errorf("tfl request failed: %s: %v", urlErr.Op, urlErr.Err)
}
