package main

import "testing"

func TestParseOLXMeResponseRecognizesShopAccounts(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		isShop bool
	}{
		{
			name:   "direct user_type",
			body:   `{"id": 1, "username": "shop", "user_type": "shop"}`,
			isShop: true,
		},
		{
			name:   "wrapped shop object",
			body:   `{"data": {"id": 1, "username": "shop", "shop": {"id": 15}}}`,
			isShop: true,
		},
		{
			name:   "regular user",
			body:   `{"id": 1, "username": "user", "user_type": "user"}`,
			isShop: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile, ok := parseOLXMeResponse([]byte(test.body))
			if !ok {
				t.Fatal("expected valid OLX profile response")
			}
			if got := profile.isShopAccount(); got != test.isShop {
				t.Fatalf("isShopAccount() = %v, want %v", got, test.isShop)
			}
		})
	}
}
