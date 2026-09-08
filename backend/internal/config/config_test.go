package config

import "testing"

func TestLoadJWTExpiry(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"3", 3}, {"", 24}, {"0", 24}, {"-2", 24}, {"invalid", 24}, {"999999999999999999999", 24},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("JWT_EXPIRE_HOUR", tc.value)
			if got := Load().JWT.ExpireHour; got != tc.want {
				t.Fatalf("expiry = %d, want %d", got, tc.want)
			}
		})
	}
}
