package live

import (
	"testing"
	"time"
)

func TestQueryKeyDeduplicatesWhitespaceAndInterval(t *testing.T) {
	first := queryKey(" sum(up) ", 30*time.Second)
	second := queryKey("sum(up)", 30*time.Second)
	if first != second {
		t.Fatalf("expected matching keys, got %q and %q", first, second)
	}

	third := queryKey("sum(up)", 15*time.Second)
	if first == third {
		t.Fatal("expected refresh interval to be part of the live query key")
	}
}

func TestClampInterval(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{name: "minimum", in: time.Second, want: minRefreshInterval},
		{name: "passes valid value", in: 30 * time.Second, want: 30 * time.Second},
		{name: "maximum", in: 10 * time.Minute, want: maxRefreshInterval},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := clampInterval(test.in); got != test.want {
				t.Fatalf("clampInterval(%s) = %s, want %s", test.in, got, test.want)
			}
		})
	}
}
