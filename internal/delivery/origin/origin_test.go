package origin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantErr   bool
		wantLocal bool
		want      []string
	}{
		{
			name:      "explicit allow list in production",
			env:       map[string]string{"ALLOWED_ORIGINS": "https://app.example.com,https://admin.example.com", "APP_ENV": "production"},
			want:      []string{"https://app.example.com", "https://admin.example.com"},
			wantLocal: false,
		},
		{
			name:      "local mode defaults to loopback",
			env:       map[string]string{"APP_ENV": "local"},
			want:      []string{"http://localhost:1111", "http://localhost:5173", "http://127.0.0.1:1111", "http://127.0.0.1:5173"},
			wantLocal: true,
		},
		{
			name:      "explicit allow list overrides local defaults",
			env:       map[string]string{"ALLOWED_ORIGINS": "http://localhost:1111", "APP_ENV": "local"},
			want:      []string{"http://localhost:1111"},
			wantLocal: true,
		},
		{
			name:    "production with empty allow list fails fast",
			env:     map[string]string{"APP_ENV": "production"},
			wantErr: true,
		},
		{
			name:    "unknown env value treated as non-local",
			env:     map[string]string{"APP_ENV": "staging", "ALLOWED_ORIGINS": ""},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.env)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if p.IsLocal != tt.wantLocal {
				t.Errorf("IsLocal = %v, want %v", p.IsLocal, tt.wantLocal)
			}
			if len(p.Allowed) != len(tt.want) {
				t.Fatalf("Allowed = %v, want %v", p.Allowed, tt.want)
			}
			for i := range tt.want {
				if p.Allowed[i] != tt.want[i] {
					t.Errorf("Allowed[%d] = %q, want %q", i, p.Allowed[i], tt.want[i])
				}
			}
		})
	}
}

func TestPolicyAllowOriginHeader(t *testing.T) {
	p, err := Parse(map[string]string{"ALLOWED_ORIGINS": "https://app.example.com", "APP_ENV": "production"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	tests := []struct {
		name   string
		origin string
		want   string
	}{
		{"allowed browser origin is reflected", "https://app.example.com", "https://app.example.com"},
		{"disallowed browser origin is empty", "https://evil.example.com", ""},
		{"empty origin (server-to-server) is empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			got := p.AllowOriginHeader(r)
			if got != tt.want {
				t.Errorf("AllowOriginHeader(origin=%q) = %q, want %q", tt.origin, got, tt.want)
			}
		})
	}
}

func TestPolicyAllowWebSocket(t *testing.T) {
	p, err := Parse(map[string]string{"ALLOWED_ORIGINS": "https://app.example.com", "APP_ENV": "production"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{"allowed browser origin upgrades", "https://app.example.com", true},
		{"disallowed browser origin rejected", "https://evil.example.com", false},
		{"empty origin (server-to-server) allowed", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/ws", nil)
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			got := p.AllowWebSocket(r)
			if got != tt.want {
				t.Errorf("AllowWebSocket(origin=%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}

func TestPolicyIsBrowser(t *testing.T) {
	p, _ := Parse(map[string]string{"ALLOWED_ORIGINS": "https://app.example.com", "APP_ENV": "production"})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if p.IsBrowser(r) {
		t.Error("expected IsBrowser=false when Origin header missing")
	}
	r.Header.Set("Origin", "https://app.example.com")
	if !p.IsBrowser(r) {
		t.Error("expected IsBrowser=true when Origin header present")
	}
	// Sanity: parse must not blow up on whitespace + empty tokens.
	if _, err := Parse(map[string]string{"ALLOWED_ORIGINS": "  ,,,  https://a.example.com  ", "APP_ENV": "production"}); err != nil {
		t.Fatalf("Parse tolerated whitespace: %v", err)
	}
	if strings.TrimSpace(p.Allowed[0]) == "" {
		t.Errorf("Allowed[0] should never be empty, got %q", p.Allowed[0])
	}
}
