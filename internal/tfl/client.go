package tfl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type StopPoint struct {
	ID         string      `json:"id"`
	CommonName string      `json:"commonName"`
	Indicator  string      `json:"indicator,omitempty"`
	StopLetter string      `json:"stopLetter,omitempty"`
	Lat        float64     `json:"lat"`
	Lon        float64     `json:"lon"`
	Modes      []string    `json:"modes,omitempty"`
	Children   []StopPoint `json:"children,omitempty"`
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

type APIError struct {
	StatusCode     int                   `json:"statusCode"`
	Message        string                `json:"message"`
	Disambiguation *DisambiguationResult `json:"disambiguation,omitempty"`
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return fmt.Sprintf("tfl http %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("tfl http %d", e.StatusCode)
}

type DisambiguationResult struct {
	FromLocationDisambiguation *Disambiguation `json:"fromLocationDisambiguation,omitempty"`
	ToLocationDisambiguation   *Disambiguation `json:"toLocationDisambiguation,omitempty"`
	ViaLocationDisambiguation  *Disambiguation `json:"viaLocationDisambiguation,omitempty"`
	JourneyVector              *JourneyVector  `json:"journeyVector,omitempty"`
}

type Disambiguation struct {
	MatchStatus           string                 `json:"matchStatus,omitempty"`
	DisambiguationOptions []DisambiguationOption `json:"disambiguationOptions,omitempty"`
}

type DisambiguationOption struct {
	ParameterValue string `json:"parameterValue,omitempty"`
	URI            string `json:"uri,omitempty"`
	Place          Place  `json:"place,omitempty"`
	MatchQuality   int    `json:"matchQuality,omitempty"`
}

type Place struct {
	NaptanID   string   `json:"naptanId,omitempty"`
	ICSCode    string   `json:"icsCode,omitempty"`
	CommonName string   `json:"commonName,omitempty"`
	PlaceType  string   `json:"placeType,omitempty"`
	Modes      []string `json:"modes,omitempty"`
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
}

type JourneyVector struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	Via  string `json:"via,omitempty"`
	URI  string `json:"uri,omitempty"`
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
	return c.StopSearchWithOptions(ctx, query, StopSearchOptions{})
}

type StopSearchOptions struct {
	Modes       []string
	Lines       []string
	MaxResults  int
	IncludeHubs bool
}

func (c *Client) StopSearchWithOptions(ctx context.Context, query string, opts StopSearchOptions) (SearchResponse, error) {
	var out SearchResponse
	params := url.Values{"query": {query}}
	setCSVParam(params, "modes", opts.Modes)
	setCSVParam(params, "lines", opts.Lines)
	if opts.MaxResults > 0 {
		params.Set("maxResults", fmt.Sprintf("%d", opts.MaxResults))
	}
	if opts.IncludeHubs {
		params.Set("includeHubs", "true")
	}
	u, err := c.endpoint("/StopPoint/Search", params)
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
	return c.getArrivals(ctx, u)
}

func (c *Client) StopPoint(ctx context.Context, stopID string) (StopPoint, error) {
	var out StopPoint
	u, err := c.endpoint("/StopPoint/"+url.PathEscape(stopID), nil)
	if err != nil {
		return out, err
	}
	return out, c.getJSON(ctx, u, &out)
}

func (c *Client) LineArrivals(ctx context.Context, stopID string, lines []string, direction, destinationStationID string) ([]Arrival, error) {
	ids := strings.Join(cleanCSV(lines), ",")
	if ids == "" {
		return c.Arrivals(ctx, stopID)
	}
	params := url.Values{}
	if direction != "" {
		params.Set("direction", direction)
	}
	if destinationStationID != "" {
		params.Set("destinationStationId", destinationStationID)
	}
	u, err := c.endpoint("/Line/"+url.PathEscape(ids)+"/Arrivals/"+url.PathEscape(stopID), params)
	if err != nil {
		return nil, err
	}
	arrivals, err := c.getArrivals(ctx, u)
	if err != nil || len(arrivals) > 0 {
		return arrivals, err
	}
	children, err := c.childStopIDs(ctx, stopID)
	if err != nil || len(children) == 0 {
		return arrivals, nil
	}
	combined := []Arrival{}
	for _, childID := range children {
		childURL, err := c.endpoint("/Line/"+url.PathEscape(ids)+"/Arrivals/"+url.PathEscape(childID), params)
		if err != nil {
			return nil, err
		}
		childArrivals, err := c.getArrivals(ctx, childURL)
		if err != nil {
			return nil, err
		}
		combined = append(combined, childArrivals...)
	}
	sort.Slice(combined, func(i, j int) bool { return combined[i].TimeToStation < combined[j].TimeToStation })
	return combined, nil
}

type JourneyOptions struct {
	Date                     string
	Time                     string
	Arriving                 bool
	Via                      string
	Preference               string
	Modes                    []string
	AccessibilityPreferences []string
	MaxTransferMinutes       string
	MaxWalkingMinutes        string
	WalkingSpeed             string
	CyclePreference          string
	IncludeAlternativeRoutes bool
	AlternativeWalking       bool
	AlternativeCycle         bool
	UseRealTimeLiveArrivals  bool
	RouteBetweenEntrances    bool
	LocalOnly                bool
}

func (c *Client) Journey(ctx context.Context, from, to string, opts JourneyOptions) (JourneyResponse, error) {
	var out JourneyResponse
	from = ResolveKnownStop(from)
	to = ResolveKnownStop(to)
	preference := opts.Preference
	if preference == "" {
		preference = "LeastTime"
	}
	params := url.Values{"journeyPreference": {preference}}
	if !opts.LocalOnly {
		params.Set("nationalSearch", "true")
	}
	if opts.Date != "" {
		params.Set("date", opts.Date)
	}
	if opts.Time != "" {
		params.Set("time", opts.Time)
	}
	if opts.Via != "" {
		params.Set("via", ResolveKnownStop(opts.Via))
	}
	if opts.Arriving {
		params.Set("timeIs", "Arriving")
	} else {
		params.Set("timeIs", "Departing")
	}
	setCSVParam(params, "mode", opts.Modes)
	setCSVParam(params, "accessibilityPreference", opts.AccessibilityPreferences)
	if opts.MaxTransferMinutes != "" {
		params.Set("maxTransferMinutes", opts.MaxTransferMinutes)
	}
	if opts.MaxWalkingMinutes != "" {
		params.Set("maxWalkingMinutes", opts.MaxWalkingMinutes)
	}
	if opts.WalkingSpeed != "" {
		params.Set("walkingSpeed", opts.WalkingSpeed)
	}
	if opts.CyclePreference != "" {
		params.Set("cyclePreference", opts.CyclePreference)
	}
	setBoolParam(params, "includeAlternativeRoutes", opts.IncludeAlternativeRoutes)
	setBoolParam(params, "alternativeWalking", opts.AlternativeWalking)
	setBoolParam(params, "alternativeCycle", opts.AlternativeCycle)
	setBoolParam(params, "useRealTimeLiveArrivals", opts.UseRealTimeLiveArrivals)
	setBoolParam(params, "routeBetweenEntrances", opts.RouteBetweenEntrances)
	path := "/Journey/JourneyResults/" + url.PathEscape(from) + "/to/" + url.PathEscape(to)
	u, err := c.endpoint(path, params)
	if err != nil {
		return out, err
	}
	err = c.getJSON(ctx, u, &out)
	return out, err
}

func (c *Client) getArrivals(ctx context.Context, endpoint string) ([]Arrival, error) {
	var raw []rawArrival
	if err := c.getJSON(ctx, endpoint, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return []Arrival{}, nil
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

func (c *Client) childStopIDs(ctx context.Context, stopID string) ([]string, error) {
	stop, err := c.StopPoint(ctx, stopID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(stop.Children))
	for _, child := range stop.Children {
		if strings.TrimSpace(child.ID) != "" {
			ids = append(ids, child.ID)
		}
	}
	return ids, nil
}

func setCSVParam(values url.Values, name string, raw []string) {
	clean := cleanCSV(raw)
	if len(clean) > 0 {
		values.Set(name, strings.Join(clean, ","))
	}
}

func cleanCSV(raw []string) []string {
	var out []string
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func setBoolParam(values url.Values, name string, enabled bool) {
	if enabled {
		values.Set(name, "true")
	}
}

func FilterArrivals(arrivals []Arrival, line, towards string) []Arrival {
	line = strings.ToLower(strings.TrimSpace(line))
	towards = strings.ToLower(strings.TrimSpace(towards))
	out := make([]Arrival, 0, len(arrivals))
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return parseAPIError(resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func parseAPIError(status int, body []byte) error {
	apiErr := &APIError{StatusCode: status, Message: http.StatusText(status)}
	if status == http.StatusMultipleChoices {
		var disambig DisambiguationResult
		if err := json.Unmarshal(body, &disambig); err == nil && hasDisambiguation(disambig) {
			apiErr.Message = "disambiguation required"
			apiErr.Disambiguation = &disambig
			return apiErr
		}
	}
	var msg struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &msg); err == nil {
		if strings.TrimSpace(msg.Message) != "" {
			apiErr.Message = msg.Message
		} else if strings.TrimSpace(msg.Error) != "" {
			apiErr.Message = msg.Error
		}
	}
	return apiErr
}

func hasDisambiguation(d DisambiguationResult) bool {
	return hasDisambiguationOptions(d.FromLocationDisambiguation) ||
		hasDisambiguationOptions(d.ToLocationDisambiguation) ||
		hasDisambiguationOptions(d.ViaLocationDisambiguation)
}

func hasDisambiguationOptions(d *Disambiguation) bool {
	return d != nil && len(d.DisambiguationOptions) > 0
}

func sanitizeRequestError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	return fmt.Errorf("tfl request failed: %s: %v", urlErr.Op, urlErr.Err)
}
