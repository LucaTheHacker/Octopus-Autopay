package config

import "testing"

func TestCardValidate(t *testing.T) {
	cases := []struct {
		name string
		card CardDetails
		ok   bool
	}{
		{"visa happy", CardDetails{Number: "4242424242424242", ExpMonth: "12", ExpYear: "30", CVC: "123", HolderName: "Foo"}, true},
		{"amex happy", CardDetails{Number: "378282246310005", ExpMonth: "12", ExpYear: "30", CVC: "1234", HolderName: "Foo"}, true},
		{"amex 3-cvc rejected", CardDetails{Number: "378282246310005", ExpMonth: "12", ExpYear: "30", CVC: "123", HolderName: "Foo"}, false},
		{"non-amex 4-cvc rejected", CardDetails{Number: "4242424242424242", ExpMonth: "12", ExpYear: "30", CVC: "1234", HolderName: "Foo"}, false},
		{"too short", CardDetails{Number: "424242", ExpMonth: "12", ExpYear: "30", CVC: "123", HolderName: "Foo"}, false},
		{"month out of range", CardDetails{Number: "4242424242424242", ExpMonth: "13", ExpYear: "30", CVC: "123", HolderName: "Foo"}, false},
		{"holder empty", CardDetails{Number: "4242424242424242", ExpMonth: "12", ExpYear: "30", CVC: "123", HolderName: " "}, false},
		{"non-digit number", CardDetails{Number: "4242 4242 4242 4242", ExpMonth: "12", ExpYear: "30", CVC: "123", HolderName: "Foo"}, false},
	}
	for _, c := range cases {
		err := c.card.Validate()
		got := err == nil
		if got != c.ok {
			t.Errorf("%s: ok=%v, err=%v", c.name, got, err)
		}
	}
}

func TestSplitExpiry(t *testing.T) {
	cases := map[string][3]string{
		"12/30":   {"12", "30", "true"},
		"12-30":   {"12", "30", "true"},
		"1230":    {"12", "30", "true"},
		"12/2030": {"12", "30", "true"},
		"":        {"", "", "false"},
		"12":      {"", "", "false"},
		"ab/cd":   {"", "", "false"},
	}
	for in, want := range cases {
		mm, yy, ok := splitExpiry(in)
		gotOK := "false"
		if ok {
			gotOK = "true"
		}
		if mm != want[0] || yy != want[1] || gotOK != want[2] {
			t.Errorf("splitExpiry(%q) = (%q, %q, %v) want %v", in, mm, yy, ok, want)
		}
	}
}

func TestStripCardNumber(t *testing.T) {
	cases := map[string]string{
		"4242 4242 4242 4242": "4242424242424242",
		"4242-4242-4242-4242": "4242424242424242",
		" 1234 5678 ":         "12345678",
		"":                    "",
	}
	for in, want := range cases {
		if got := stripCardNumber(in); got != want {
			t.Errorf("stripCardNumber(%q) = %q, want %q", in, got, want)
		}
	}
}
