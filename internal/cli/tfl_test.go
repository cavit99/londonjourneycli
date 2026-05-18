package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cavit99/londonjourneycli/internal/exitcode"
)

func TestTFLCommandsWithFakeServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/StopPoint/Search", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"Query\":\"London Bridge\",\"Total\":1,\"Matches\":[{\"ID\":\"490000139R\",\"Name\":\"London Bridge Station\",\"Lat\":51.5,\"Lon\":-0.08,\"Modes\":[\"bus\"]}]}"))
	})
	mux.HandleFunc("/StopPoint/490000139R/Arrivals", func(w http.ResponseWriter, r *http.Request) {
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
		{name: "search", args: []string{"--json", "tfl", "stop-search", "London Bridge", "--limit", "1"}, want: "London Bridge Station", code: exitcode.OK},
		{name: "arrivals", args: []string{"tfl", "arrivals", "--stop", "490000139R", "--line", "43"}, want: "Friern Barnet", code: exitcode.OK},
		{name: "journey", args: []string{"tfl", "journey", "--from", "London Bridge", "--to", "Paddington"}, want: "Option 1", code: exitcode.OK},
		{name: "watch", args: []string{"--json", "tfl", "watch-arrival", "--stop", "490000139R", "--line", "43", "--threshold", "2m", "--dry-run"}, want: "\"status\": \"due\"", code: exitcode.OK},
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

func TestTFLWatchArrivalSuccessfulOpenClawDelivery(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/StopPoint/490000139R/Arrivals", func(w http.ResponseWriter, r *http.Request) {
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
			mux.HandleFunc("/StopPoint/490000139R/Arrivals", func(w http.ResponseWriter, r *http.Request) {
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
				"--json", "tfl", "watch-arrival",
				"--stop", "490000139R",
				"--line", "43",
				"--openclaw-channel", "whatsapp",
				"--openclaw-target", "+15555550123",
			}, &out, &errb)
			if code != exitcode.Generic {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), errb.String())
			}
			if !strings.Contains(out.String(), "\"status\": \""+tc.status+"\"") || !strings.Contains(out.String(), "\"notificationOk\": false") || !strings.Contains(out.String(), "\"notificationError\":") {
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
