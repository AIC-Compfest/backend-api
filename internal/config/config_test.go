package config

import "testing"

func TestValidateSupabase(t *testing.T) {
	tests := []struct{name string; cfg Config; wantErr bool}{
		{"valid", Config{SupabaseURL:"https://example.supabase.co", SupabasePublishableKey:"key"}, false},
		{"missing URL", Config{SupabasePublishableKey:"key"}, true},
		{"missing key", Config{SupabaseURL:"https://example.supabase.co"}, true},
	}
	for _, tt := range tests { t.Run(tt.name, func(t *testing.T) { if err := tt.cfg.ValidateSupabase(); (err != nil) != tt.wantErr { t.Fatalf("ValidateSupabase() error = %v", err) } }) }
}
