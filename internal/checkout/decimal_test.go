package checkout

import "testing"

// decimalToPesewas is the one place a NUMERIC(12,2) quote turns into the
// integer pesewas Paystack is actually charged — no DB or provider needed to
// exercise it directly, and it's the highest-stakes arithmetic in the
// package, so it gets a plain table test rather than only incidental
// coverage through InitiateCheckout's happy path.
func TestDecimalToPesewas(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "4500.50", want: 450050},
		{in: "4500.00", want: 450000},
		{in: "4500", want: 450000},
		{in: "4500.5", want: 450050},
		{in: "4500.567", want: 450056}, // truncated to 2 decimal places, not rounded
		{in: "0.01", want: 1},
		{in: "0", want: 0},
		{in: "-500.00", want: -50000},
		{in: "  500.00  ", want: 50000},
		{in: "", wantErr: true},
		{in: "not-a-number", wantErr: true},
		{in: "500.xx", wantErr: true},
	}
	for _, c := range cases {
		got, err := decimalToPesewas(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("decimalToPesewas(%q) = %d, nil; want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("decimalToPesewas(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("decimalToPesewas(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
