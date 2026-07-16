package usersignup

import (
	"testing"
)

func strPtr(s string) *string {
	return &s
}

func TestSignupTokenEmailMatchesDomain(t *testing.T) {
	tests := []struct {
		name        string
		emailDomain *string
		email       string
		want        bool
	}{
		{name: "no restriction allows any email", emailDomain: nil, email: "user@anything.com", want: true},
		{name: "empty restriction allows any email", emailDomain: strPtr(""), email: "user@anything.com", want: true},
		{name: "matching domain", emailDomain: strPtr("example.com"), email: "user@example.com", want: true},
		{name: "matching domain case-insensitive", emailDomain: strPtr("example.com"), email: "User@Example.COM", want: true},
		{name: "non-matching domain", emailDomain: strPtr("example.com"), email: "user@other.com", want: false},
		{name: "subdomain does not match", emailDomain: strPtr("example.com"), email: "user@mail.example.com", want: false},
		{name: "domain suffix does not match", emailDomain: strPtr("example.com"), email: "user@notexample.com", want: false},
		{name: "missing @ with restriction", emailDomain: strPtr("example.com"), email: "userexample.com", want: false},
		{name: "empty email with restriction", emailDomain: strPtr("example.com"), email: "", want: false},
		{name: "plus addressing still matches", emailDomain: strPtr("example.com"), email: "user+tag@example.com", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &SignupToken{EmailDomain: tc.emailDomain}
			got := st.EmailMatchesDomain(tc.email)
			if got != tc.want {
				t.Errorf("EmailMatchesDomain(%q) with domain %v = %v, want %v", tc.email, tc.emailDomain, got, tc.want)
			}
		})
	}
}

func TestNormalizeEmailDomain(t *testing.T) {
	tests := []struct {
		name    string
		input   *string
		want    *string
		wantErr bool
	}{
		{name: "nil returns nil", input: nil, want: nil},
		{name: "empty string returns nil", input: strPtr(""), want: nil},
		{name: "whitespace only returns nil", input: strPtr("   "), want: nil},
		{name: "simple domain", input: strPtr("example.com"), want: strPtr("example.com")},
		{name: "trims and lowercases", input: strPtr("  Example.COM "), want: strPtr("example.com")},
		{name: "strips leading @", input: strPtr("@example.com"), want: strPtr("example.com")},
		{name: "multi-level domain", input: strPtr("mail.example.co.uk"), want: strPtr("mail.example.co.uk")},
		{name: "invalid: no tld", input: strPtr("example"), wantErr: true},
		{name: "invalid: spaces inside", input: strPtr("exa mple.com"), wantErr: true},
		{name: "invalid: has scheme", input: strPtr("http://example.com"), wantErr: true},
		{name: "invalid: trailing dot", input: strPtr("example.com."), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeEmailDomain(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeEmailDomain(%v) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeEmailDomain(%v) unexpected error: %v", tc.input, err)
			}
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("normalizeEmailDomain(%v) = %q, want nil", tc.input, *got)
			case tc.want != nil && got == nil:
				t.Errorf("normalizeEmailDomain(%v) = nil, want %q", tc.input, *tc.want)
			case tc.want != nil && got != nil && *got != *tc.want:
				t.Errorf("normalizeEmailDomain(%v) = %q, want %q", tc.input, *got, *tc.want)
			}
		})
	}
}
