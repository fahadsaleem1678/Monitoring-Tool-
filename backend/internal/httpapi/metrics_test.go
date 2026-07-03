package httpapi

import (
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestParseRangeRequestRejectsTooManySamples(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/metrics/query-range?query=up&start=0&end=86400&step=1", nil)

	_, err := parseRangeRequest(req)
	if err == nil {
		t.Fatal("expected sample limit error")
	}
}

func TestParseRangeRequestAcceptsValidRequest(t *testing.T) {
	end := time.Now().UTC()
	start := end.Add(-time.Hour)
	url := "/api/v1/metrics/query-range?query=up&start=" + formatTestTime(start) + "&end=" + formatTestTime(end) + "&step=60"
	req := httptest.NewRequest("GET", url, nil)

	parsed, err := parseRangeRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Query != "up" {
		t.Fatalf("query = %q, want up", parsed.Query)
	}
	if parsed.Step != time.Minute {
		t.Fatalf("step = %s, want 1m", parsed.Step)
	}
}

func TestQueryFromRequestRequiresQuery(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/metrics/query", nil)

	_, err := queryFromRequest(req)
	if err == nil {
		t.Fatal("expected query required error")
	}
}

func formatTestTime(value time.Time) string {
	return strconv.FormatFloat(float64(value.UnixMilli())/1000, 'f', 3, 64)
}
