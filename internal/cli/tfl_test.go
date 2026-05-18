package cli

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cavit99/londonjourneycli/internal/exitcode"
)

func TestTFLCommandsWithFakeServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/Line/Mode/tube,dlr,elizabeth-line,overground,tram/Status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[{\"id\":\"victoria\",\"name\":\"Victoria\",\"modeName\":\"tube\",\"lineStatuses\":[{\"statusSeverity\":10,\"statusSeverityDescription\":\"Good Service\"}]}]"))
	})
	mux.HandleFunc("/Line/victoria/Status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[{\"id\":\"victoria\",\"name\":\"Victoria\",\"modeName\":\"tube\",\"lineStatuses\":[{\"statusSeverity\":6,\"statusSeverityDescription\":\"Severe Delays\",\"disruption\":{\"category\":\"RealTime\",\"type\":\"lineInfo\",\"description\":\"Minor platform crowding\",\"closureText\":\"minorDelays\"}}]}]"))
	})
	mux.HandleFunc("/Line/victoria/Route", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[{\"id\":\"victoria\",\"name\":\"Victoria\",\"modeName\":\"tube\",\"routeSections\":[{\"direction\":\"inbound\",\"originationName\":\"Walthamstow Central\",\"destinationName\":\"Brixton\",\"originator\":\"940GZZLUWWL\",\"destination\":\"940GZZLUBXN\",\"serviceType\":\"Regular\"}]}]"))
	})
	mux.HandleFunc("/StopPoint/Search", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"Query\":\"London Bridge\",\"Total\":1,\"Matches\":[{\"ID\":\"490000139R\",\"Name\":\"London Bridge Station\",\"Lat\":51.5,\"Lon\":-0.08,\"Modes\":[\"bus\"]}]}"))
	})
	mux.HandleFunc("/StopPoint", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("modes"); got != "bus" {
			t.Fatalf("modes=%q", got)
		}
		if got := r.URL.Query().Get("stopTypes"); got != "NaptanPublicBusCoachTram" {
			t.Fatalf("stopTypes=%q", got)
		}
		if r.URL.Query().Get("radius") == "1" {
			_, _ = w.Write([]byte("{\"stopPoints\":[]}"))
			return
		}
		_, _ = w.Write([]byte("{\"stopPoints\":[{\"naptanId\":\"490FAR\",\"commonName\":\"Far Stop\",\"lat\":51.51,\"lon\":-0.09,\"distance\":100,\"modes\":[\"bus\"]},{\"id\":\"490000139R\",\"commonName\":\"London Bridge Bus Station\",\"indicator\":\"Stop D\",\"stopLetter\":\"D\",\"lat\":51.5,\"lon\":-0.08,\"distance\":42,\"modes\":[\"bus\"]}]}"))
	})
	mux.HandleFunc("/StopPoint/490000139R/Arrivals", func(w http.ResponseWriter, r *http.Request) {
		body := "[{\"LineName\":\"43\",\"DestinationName\":\"Friern Barnet\",\"StationName\":\"London Bridge Bus Station\",\"PlatformName\":\"D\",\"Towards\":\"Old Street\",\"ExpectedArrival\":\"2026-05-18T01:01:00Z\",\"TimeToStation\":60,\"VehicleID\":\"b\"}]"
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/StopPoint/490G00008459", func(w http.ResponseWriter, r *http.Request) {
		body := "{\"id\":\"490G00008459\",\"commonName\":\"Ildersly Grove\",\"children\":[{\"id\":\"490008459S\",\"commonName\":\"Ildersly Grove\",\"indicator\":\"Stop WH\",\"stopLetter\":\"WH\",\"lat\":51.43,\"lon\":-0.09,\"modes\":[\"bus\"]}]}"
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/Line/43/Arrivals/490000139R", func(w http.ResponseWriter, r *http.Request) {
		body := "[{\"LineName\":\"43\",\"DestinationName\":\"Friern Barnet\",\"StationName\":\"London Bridge Bus Station\",\"PlatformName\":\"D\",\"Towards\":\"Old Street\",\"ExpectedArrival\":\"2026-05-18T01:01:00Z\",\"TimeToStation\":60,\"VehicleID\":\"b\"}]"
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/Journey/JourneyResults/1000139/to/1000174", func(w http.ResponseWriter, r *http.Request) {
		body := "{\"Journeys\":[{\"StartDateTime\":\"2026-05-18T01:00:00\",\"ArrivalDateTime\":\"2026-05-18T01:30:00\",\"Duration\":30,\"Legs\":[{\"Mode\":{\"Name\":\"tube\"},\"DepartureTime\":\"2026-05-18T01:00:00\",\"ArrivalTime\":\"2026-05-18T01:30:00\",\"DeparturePoint\":{\"CommonName\":\"London Bridge\"},\"ArrivalPoint\":{\"CommonName\":\"Paddington\"},\"RouteOptions\":[{\"Name\":\"Jubilee\"}]}]}]}"
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("TFL_BASE_URL", srv.URL)

	cases := []struct {
		name string
		args []string
		want string
		code int
	}{
		{name: "status", args: []string{"--json", "tfl", "status"}, want: "Good Service", code: exitcode.OK},
		{name: "status plain", args: []string{"--plain", "tfl", "status"}, want: "victoria\tVictoria\ttube\t10\tGood Service", code: exitcode.OK},
		{name: "status output projection", args: []string{"--json", "--output", "0.lineStatuses.0.statusSeverityDescription", "tfl", "status"}, want: "\"Good Service\"", code: exitcode.OK},
		{name: "status output envelope", args: []string{"--json", "--envelope", "--output", "0.lineStatuses.0.statusSeverityDescription", "tfl", "status"}, want: "\"ok\": true", code: exitcode.OK},
		{name: "status output missing path", args: []string{"--json", "--output", "0.nope", "tfl", "status"}, want: "output path", code: exitcode.NoData},
		{name: "status line", args: []string{"--json", "tfl", "status", "--line", "victoria"}, want: "Severe Delays", code: exitcode.OK},
		{name: "disruptions", args: []string{"--plain", "tfl", "disruptions", "--line", "victoria"}, want: "victoria\tVictoria\tRealTime\tlineInfo\tMinor platform crowding", code: exitcode.OK},
		{name: "disruptions empty json", args: []string{"--json", "tfl", "disruptions"}, want: "[]", code: exitcode.OK},
		{name: "disruptions empty human", args: []string{"tfl", "disruptions"}, want: "No active disruptions found.", code: exitcode.OK},
		{name: "line routes", args: []string{"--json", "tfl", "line-routes", "--line", "victoria"}, want: "Walthamstow Central", code: exitcode.OK},
		{name: "nearby stops", args: []string{"--json", "tfl", "nearby-stops", "--lat", "51.505", "--lon", "-0.087", "--limit", "1"}, want: "London Bridge Bus Station", code: exitcode.OK},
		{name: "nearby naptan fallback", args: []string{"--json", "tfl", "nearby-stops", "--lat", "51.505", "--lon", "-0.087", "--limit", "2"}, want: "490FAR", code: exitcode.OK},
		{name: "nearby meridian", args: []string{"--json", "tfl", "nearby-stops", "--lat", "51.48", "--lon", "0", "--limit", "1"}, want: "London Bridge Bus Station", code: exitcode.OK},
		{name: "nearby plain", args: []string{"--plain", "tfl", "nearby-stops", "--lat", "51.505", "--lon", "-0.087", "--limit", "1"}, want: "490000139R\tLondon Bridge Bus Station\tStop D\tD\t42", code: exitcode.OK},
		{name: "nearby empty human", args: []string{"tfl", "nearby-stops", "--lat", "51.505", "--lon", "-0.087", "--radius", "1"}, want: "No nearby stops found.", code: exitcode.OK},
		{name: "search", args: []string{"--json", "tfl", "stop-search", "London Bridge", "--limit", "1"}, want: "London Bridge Station", code: exitcode.OK},
		{name: "stop-info", args: []string{"--json", "tfl", "stop-info", "--stop", "490G00008459"}, want: "490008459S", code: exitcode.OK},
		{name: "stop-info output projection", args: []string{"--json", "--output", "id", "tfl", "stop-info", "--stop", "490G00008459"}, want: "\"490G00008459\"", code: exitcode.OK},
		{name: "arrivals", args: []string{"tfl", "arrivals", "--stop", "490000139R", "--line", "43"}, want: "Friern Barnet", code: exitcode.OK},
		{name: "arrivals query line", args: []string{"--json", "tfl", "arrivals", "--query", "London Bridge", "--line", "43"}, want: "\"resolvedStop\": {", code: exitcode.OK},
		{name: "arrivals query no line", args: []string{"--json", "tfl", "arrivals", "--query", "London Bridge"}, want: "\"status\": \"ok\"", code: exitcode.OK},
		{name: "next arrival query", args: []string{"--json", "tfl", "next-arrival", "--query", "London Bridge", "--line", "43"}, want: "\"resolvedStop\": {", code: exitcode.OK},
		{name: "journey", args: []string{"tfl", "journey", "--from", "London Bridge", "--to", "Paddington"}, want: "Option 1", code: exitcode.OK},
		{name: "watch", args: []string{"--json", "tfl", "watch-arrival", "--stop", "490000139R", "--line", "43", "--threshold", "2m", "--dry-run"}, want: "\"status\": \"due\"", code: exitcode.OK},
		{name: "watch query", args: []string{"--json", "tfl", "watch-arrival", "--query", "London Bridge", "--line", "43", "--threshold", "2m", "--dry-run"}, want: "London Bridge Station", code: exitcode.OK},
		{name: "watch plain", args: []string{"--plain", "tfl", "watch-arrival", "--stop", "490000139R", "--line", "43", "--threshold", "2m", "--dry-run"}, want: "due\tTransport heads-up:", code: exitcode.OK},
		{name: "watch documented no-input", args: []string{"--json", "--no-input", "tfl", "watch-arrival", "--stop", "490000139R", "--line", "43", "--threshold", "2m", "--dry-run"}, want: "\"status\": \"due\"", code: exitcode.OK},
		{name: "negative search limit", args: []string{"tfl", "stop-search", "London Bridge", "--limit", "-1"}, want: "--limit must be >= 0", code: exitcode.Usage},
		{name: "negative arrivals limit", args: []string{"tfl", "arrivals", "--stop", "490000139R", "--limit", "-1"}, want: "--limit must be >= 0", code: exitcode.Usage},
		{name: "negative watch threshold", args: []string{"tfl", "watch-arrival", "--stop", "490000139R", "--line", "43", "--threshold", "-1m"}, want: "--threshold must be > 0", code: exitcode.Usage},
		{name: "no-input requires delivery", args: []string{"--no-input", "tfl", "watch-arrival", "--stop", "490000139R", "--line", "43"}, want: "requires visible OpenClaw delivery", code: exitcode.Config},
		{name: "partial delivery rejected", args: []string{"tfl", "watch-arrival", "--stop", "490000139R", "--line", "43", "--openclaw-channel", "whatsapp"}, want: "must be provided together", code: exitcode.Config},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			code := Run(context.Background(), tc.args, &out, &errb)
			if code != tc.code {
				t.Fatalf("code=%d stderr=%s stdout=%s", code, errb.String(), out.String())
			}
			combined := out.String() + errb.String()
			if !strings.Contains(combined, tc.want) {
				t.Fatalf("expected %q in %s", tc.want, combined)
			}
		})
	}

	_ = os.Getenv("TFL_BASE_URL")
}

func TestTFLNextArrivalQuerySkipsSearchMatchesWithNoPredictions(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/StopPoint/Search", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("lines"); got != "43" {
			t.Fatalf("lines query=%q", got)
		}
		if got := r.URL.Query().Get("modes"); got != "bus" {
			t.Fatalf("modes query=%q", got)
		}
		body := `{"Query":"London Bridge","Total":2,"Matches":[{"ID":"first","Name":"Wrong Side","Lat":51.5,"Lon":-0.08,"Modes":["bus"]},{"ID":"second","Name":"Right Side","Lat":51.5,"Lon":-0.08,"Modes":["bus"]}]}`
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/Line/43/Arrivals/first", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[]"))
	})
	mux.HandleFunc("/Line/43/Arrivals/second", func(w http.ResponseWriter, r *http.Request) {
		body := `[{"LineName":"43","DestinationName":"Friern Barnet","StationName":"Right Side","PlatformName":"D","Towards":"Old Street","ExpectedArrival":"2026-05-18T01:03:00Z","TimeToStation":180,"VehicleID":"b"}]`
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("TFL_BASE_URL", srv.URL)

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--json", "tfl", "next-arrival", "--query", "London Bridge", "--line", "43"}, &out, &errb)
	if code != exitcode.OK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	for _, want := range []string{`"status": "ok"`, `"name": "Right Side"`, `"destinationName": "Friern Barnet"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in %s", want, out.String())
		}
	}
}

func TestTFLNextArrivalQueryContinuesAfterCandidateError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/StopPoint/Search", func(w http.ResponseWriter, r *http.Request) {
		body := `{"Query":"London Bridge","Total":2,"Matches":[{"ID":"broken","Name":"Broken Candidate","Modes":["bus"]},{"ID":"second","Name":"Right Side","Modes":["bus"]}]}`
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/Line/43/Arrivals/broken", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream failed", http.StatusBadGateway)
	})
	mux.HandleFunc("/Line/43/Arrivals/second", func(w http.ResponseWriter, r *http.Request) {
		body := `[{"LineName":"43","DestinationName":"Friern Barnet","StationName":"Right Side","PlatformName":"D","Towards":"Old Street","ExpectedArrival":"2026-05-18T01:03:00Z","TimeToStation":180,"VehicleID":"b"}]`
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("TFL_BASE_URL", srv.URL)

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--json", "tfl", "next-arrival", "--query", "London Bridge", "--line", "43"}, &out, &errb)
	if code != exitcode.OK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), `"name": "Right Side"`) {
		t.Fatalf("expected second candidate to win, got %s", out.String())
	}
}

func TestTFLNextArrivalQueryReportsAPIErrorWhenCandidateErrorLeavesResultAmbiguous(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/StopPoint/Search", func(w http.ResponseWriter, r *http.Request) {
		body := `{"Query":"London Bridge","Total":2,"Matches":[{"ID":"broken","Name":"Broken Candidate","Modes":["bus"]},{"ID":"empty","Name":"Empty Candidate","Modes":["bus"]}]}`
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/Line/43/Arrivals/broken", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream failed", http.StatusBadGateway)
	})
	mux.HandleFunc("/Line/43/Arrivals/empty", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[]"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("TFL_BASE_URL", srv.URL)

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--json", "tfl", "next-arrival", "--query", "London Bridge", "--line", "43"}, &out, &errb)
	if code != exitcode.Network {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	for _, want := range []string{`"status": "api_error"`, `"statusCode": 502`, "Broken Candidate"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in %s", want, out.String())
		}
	}
	if strings.TrimSpace(errb.String()) != "" {
		t.Fatalf("expected empty stderr for JSON API error, got %s", errb.String())
	}
}

func TestTFLNextArrivalQueryStopNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/StopPoint/Search", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Query":"Nowhere","Total":0,"Matches":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("TFL_BASE_URL", srv.URL)

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--json", "tfl", "next-arrival", "--query", "Nowhere", "--line", "43"}, &out, &errb)
	if code != exitcode.NoData {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	for _, want := range []string{`"status": "stop_not_found"`, `"query": "Nowhere"`, "TfL found no stop matching"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in %s", want, out.String())
		}
	}
}

func TestTFLArrivalsQueryStopNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/StopPoint/Search", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"Query\":\"Nowhere\",\"Total\":0,\"Matches\":[]}"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("TFL_BASE_URL", srv.URL)

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--json", "tfl", "arrivals", "--query", "Nowhere", "--line", "43"}, &out, &errb)
	if code != exitcode.NoData {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	for _, want := range []string{"\"status\": \"stop_not_found\"", "\"query\": \"Nowhere\"", "TfL found no stop matching"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in %s", want, out.String())
		}
	}
	if strings.TrimSpace(errb.String()) != "" {
		t.Fatalf("expected empty stderr for JSON no-data, got %s", errb.String())
	}
}

func TestTFLArrivalsQueryNoData(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/StopPoint/Search", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"Query\":\"London Bridge\",\"Total\":1,\"Matches\":[{\"ID\":\"490000139R\",\"Name\":\"London Bridge Station\",\"Modes\":[\"bus\"]}]}"))
	})
	mux.HandleFunc("/Line/43/Arrivals/490000139R", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[]"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("TFL_BASE_URL", srv.URL)

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--json", "tfl", "arrivals", "--query", "London Bridge", "--line", "43"}, &out, &errb)
	if code != exitcode.NoData {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	for _, want := range []string{"\"status\": \"no_data\"", "\"resolvedStop\": {", "no matching arrivals"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in %s", want, out.String())
		}
	}
	if strings.TrimSpace(errb.String()) != "" {
		t.Fatalf("expected empty stderr for JSON no-data, got %s", errb.String())
	}
}

func TestTFLEnvelopeWrapsRequestFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TFL_BASE_URL", "http://"+addr)

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--json", "--envelope", "--timeout", "1s", "tfl", "status"}, &out, &errb)
	if code != exitcode.Network {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	for _, want := range []string{"\"ok\": false", "\"status\": \"api_failed\"", "\"data\": {"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in %s", want, out.String())
		}
	}
	if strings.TrimSpace(errb.String()) != "" {
		t.Fatalf("expected empty stderr for JSON request failure, got %s", errb.String())
	}
}

func TestTFLWatchArrivalQueryStopNotFoundIsVisible(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/StopPoint/Search", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Query":"Nowhere","Total":0,"Matches":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("TFL_BASE_URL", srv.URL)

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--json", "tfl", "watch-arrival", "--query", "Nowhere", "--line", "43", "--dry-run"}, &out, &errb)
	if code != exitcode.NoData {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	for _, want := range []string{`"status": "stop_not_found"`, `"notificationOk": true`, "TfL found no stop matching"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in %s", want, out.String())
		}
	}
}

func TestTFLWatchArrivalSuccessfulOpenClawDelivery(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/StopPoint/490000139R/Arrivals", func(w http.ResponseWriter, r *http.Request) {
		body := "[{\"LineName\":\"43\",\"DestinationName\":\"Friern Barnet\",\"StationName\":\"London Bridge Bus Station\",\"PlatformName\":\"D\",\"Towards\":\"Old Street\",\"ExpectedArrival\":\"2026-05-18T01:01:00Z\",\"TimeToStation\":60,\"VehicleID\":\"b\"}]"
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/Line/43/Arrivals/490000139R", func(w http.ResponseWriter, r *http.Request) {
		body := "[{\"LineName\":\"43\",\"DestinationName\":\"Friern Barnet\",\"StationName\":\"London Bridge Bus Station\",\"PlatformName\":\"D\",\"Towards\":\"Old Street\",\"ExpectedArrival\":\"2026-05-18T01:01:00Z\",\"TimeToStation\":60,\"VehicleID\":\"b\"}]"
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("TFL_BASE_URL", srv.URL)

	binDir := t.TempDir()
	logPath := binDir + string(os.PathSeparator) + "argv.log"
	openclaw := binDir + string(os.PathSeparator) + "openclaw"
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"" + logPath + "\"\nexit 0\n"
	if err := os.WriteFile(openclaw, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{
		"--json", "--no-input", "tfl", "watch-arrival",
		"--stop", "490000139R",
		"--line", "43",
		"--openclaw-channel", "whatsapp",
		"--openclaw-target", "+15555550123",
	}, &out, &errb)
	if code != exitcode.OK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "\"notificationOk\": true") {
		t.Fatalf("expected notificationOk true, got %s", out.String())
	}
	argv, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(argv)
	for _, want := range []string{"message send", "--channel whatsapp", "--target +15555550123", "--message Transport heads-up:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in recorded argv %q", want, got)
		}
	}
}

func TestTFLWatchArrivalReportsNotificationFailure(t *testing.T) {
	binDir := t.TempDir()
	openclaw := binDir + string(os.PathSeparator) + "openclaw"
	if err := os.WriteFile(openclaw, []byte("#!/bin/sh\necho send failed >&2\nexit 42\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cases := []struct {
		name     string
		response string
		status   string
	}{
		{name: "api_failed", response: "error", status: "api_failed"},
		{name: "no_data", response: "[]", status: "no_data"},
		{name: "due", response: "[{\"LineName\":\"43\",\"DestinationName\":\"Friern Barnet\",\"StationName\":\"London Bridge Bus Station\",\"PlatformName\":\"D\",\"Towards\":\"Old Street\",\"ExpectedArrival\":\"2026-05-18T01:01:00Z\",\"TimeToStation\":60,\"VehicleID\":\"b\"}]", status: "due"},
		{name: "delayed", response: "[{\"LineName\":\"43\",\"DestinationName\":\"Friern Barnet\",\"StationName\":\"London Bridge Bus Station\",\"PlatformName\":\"D\",\"Towards\":\"Old Street\",\"ExpectedArrival\":\"2026-05-18T01:10:00Z\",\"TimeToStation\":600,\"VehicleID\":\"b\"}]", status: "delayed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/Line/43/Arrivals/490000139R", func(w http.ResponseWriter, r *http.Request) {
				if tc.response == "error" {
					http.Error(w, "upstream failed", http.StatusBadGateway)
					return
				}
				_, _ = w.Write([]byte(tc.response))
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			t.Setenv("TFL_BASE_URL", srv.URL)

			var out, errb bytes.Buffer
			code := Run(context.Background(), []string{
				"--json", "--envelope", "tfl", "watch-arrival",
				"--stop", "490000139R",
				"--line", "43",
				"--openclaw-channel", "whatsapp",
				"--openclaw-target", "+15555550123",
			}, &out, &errb)
			if code != exitcode.Generic {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
			}
			if !strings.Contains(out.String(), "\"ok\": false") || !strings.Contains(out.String(), "\"status\": \""+tc.status+"\"") || !strings.Contains(out.String(), "\"notificationOk\": false") || !strings.Contains(out.String(), "\"notificationError\":") {
				t.Fatalf("expected failed notification result, got %s", out.String())
			}
			if !strings.Contains(errb.String(), "openclaw message send") {
				t.Fatalf("expected send failure diagnostic, got %s", errb.String())
			}
		})
	}
}

func TestTFLWatchArrivalPlainNotificationFailureIsSingleTSVRow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/StopPoint/490000139R/Arrivals", func(w http.ResponseWriter, r *http.Request) {
		body := "[{\"LineName\":\"43\",\"DestinationName\":\"Friern Barnet\",\"StationName\":\"London Bridge Bus Station\",\"PlatformName\":\"D\",\"Towards\":\"Old Street\",\"ExpectedArrival\":\"2026-05-18T01:01:00Z\",\"TimeToStation\":60,\"VehicleID\":\"b\"}]"
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/Line/43/Arrivals/490000139R", func(w http.ResponseWriter, r *http.Request) {
		body := "[{\"LineName\":\"43\",\"DestinationName\":\"Friern Barnet\",\"StationName\":\"London Bridge Bus Station\",\"PlatformName\":\"D\",\"Towards\":\"Old Street\",\"ExpectedArrival\":\"2026-05-18T01:01:00Z\",\"TimeToStation\":60,\"VehicleID\":\"b\"}]"
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("TFL_BASE_URL", srv.URL)

	binDir := t.TempDir()
	openclaw := binDir + string(os.PathSeparator) + "openclaw"
	if err := os.WriteFile(openclaw, []byte("#!/bin/sh\necho 'send failed' >&2\necho 'second line' >&2\nexit 42\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{
		"--plain", "tfl", "watch-arrival",
		"--stop", "490000139R",
		"--line", "43",
		"--openclaw-channel", "whatsapp",
		"--openclaw-target", "+15555550123",
	}, &out, &errb)
	if code != exitcode.Generic {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one TSV row, got %d rows: %q", len(lines), out.String())
	}
	fields := strings.Split(lines[0], "\t")
	if len(fields) != 11 {
		t.Fatalf("expected 11 TSV fields, got %d in %q", len(fields), lines[0])
	}
	if fields[0] != "due" || fields[3] != "false" {
		t.Fatalf("unexpected status/notification fields: %q", lines[0])
	}
	if strings.Contains(fields[4], "\n") || strings.Contains(fields[4], "\t") {
		t.Fatalf("notification error was not sanitized: %q", fields[4])
	}
}

func TestTFLJourneyAmbiguityJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/Journey/JourneyResults/1000139/to/Highgate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMultipleChoices)
		_, _ = w.Write([]byte(`{"toLocationDisambiguation":{"matchStatus":"list","disambiguationOptions":[{"parameterValue":"1000109","uri":"/journey/journeyresults/1000139/to/1000109","place":{"commonName":"Highgate (London), Highgate","placeType":"StopPoint","naptanId":"490000109S","icsCode":"1000109","modes":["tube","bus"],"lat":51.5777,"lon":-0.1457},"matchQuality":1000}]},"fromLocationDisambiguation":{"matchStatus":"identified"},"journeyVector":{"from":"1000139","to":"Highgate"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("TFL_BASE_URL", srv.URL)

	var out, errb bytes.Buffer
	code := Run(context.Background(), []string{"--json", "tfl", "journey", "--from", "London Bridge", "--to", "Highgate"}, &out, &errb)
	if code != exitcode.Usage {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	for _, want := range []string{`"status": "ambiguous"`, `"statusCode": 300`, "Highgate (London), Highgate", "Resolve the origin"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in %s", want, out.String())
		}
	}
	if strings.TrimSpace(errb.String()) != "" {
		t.Fatalf("expected empty stderr for JSON ambiguity, got %s", errb.String())
	}

	out.Reset()
	errb.Reset()
	code = Run(context.Background(), []string{"--json", "--output", "journeys.0.duration", "tfl", "journey", "--from", "London Bridge", "--to", "Highgate"}, &out, &errb)
	if code != exitcode.Usage {
		t.Fatalf("output ambiguity code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), `"status": "ambiguous"`) || strings.TrimSpace(errb.String()) != "" {
		t.Fatalf("expected full structured ambiguity despite projection miss, stdout=%s stderr=%s", out.String(), errb.String())
	}
	out.Reset()
	errb.Reset()
	code = Run(context.Background(), []string{"--json", "--envelope", "--output", "journeys.0.duration", "tfl", "journey", "--from", "London Bridge", "--to", "Highgate"}, &out, &errb)
	if code != exitcode.Usage {
		t.Fatalf("enveloped output ambiguity code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), `"ok": false`) || !strings.Contains(out.String(), `"status": "ambiguous"`) {
		t.Fatalf("expected false envelope with full structured ambiguity, stdout=%s", out.String())
	}
	out.Reset()
	errb.Reset()
	code = Run(context.Background(), []string{"tfl", "journey", "--from", "London Bridge", "--to", "Highgate"}, &out, &errb)
	if code != exitcode.Usage {
		t.Fatalf("plain code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "disambiguation required") {
		t.Fatalf("expected plain ambiguity diagnostic, got %s", errb.String())
	}
}
