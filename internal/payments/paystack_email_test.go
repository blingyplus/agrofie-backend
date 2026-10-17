package payments

import "testing"

func TestPaystackSafeEmail(t *testing.T) {
	cases := map[string]string{
		"demo.organizer@agrofie.local": "demo.organizer@agrofie.app",
		"a@b.test":                     "a@agrofie.app",
		"x@localhost":                  "x@agrofie.app",
		"real@gmail.com":               "real@gmail.com",
		"nonsense":                     "nonsense",
	}
	for in, want := range cases {
		if got := paystackSafeEmail(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
