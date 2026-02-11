package scheduler

import "testing"

func TestFormatFloat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   float64
		want string
	}{
		{name: "positive with decimals", in: 4.25, want: "4.25"},
		{name: "rounding", in: 1.236, want: "1.24"},
		{name: "integer", in: 10, want: "10.00"},
		{name: "negative", in: -3.5, want: "-3.50"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := formatFloat(tc.in); got != tc.want {
				t.Fatalf("formatFloat(%v): got %q want %q", tc.in, got, tc.want)
			}
		})
	}
}
