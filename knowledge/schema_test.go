package knowledge

import "testing"

func TestIsStale(t *testing.T) {
	today := Today()

	tests := []struct {
		name  string
		entry Entry
		want  bool
	}{
		{
			name:  "no expiry date",
			entry: Entry{},
			want:  false,
		},
		{
			name:  "expiry in future",
			entry: Entry{ExpiresAt: "2099-01-01"},
			want:  false,
		},
		{
			name:  "expiry in past, no last verified",
			entry: Entry{ExpiresAt: "2020-01-01"},
			want:  true,
		},
		{
			name:  "expiry in past, last verified before expiry",
			entry: Entry{ExpiresAt: "2020-06-01", LastVerified: "2020-01-01"},
			want:  true,
		},
		{
			name:  "expiry in past, last verified after expiry",
			entry: Entry{ExpiresAt: "2020-01-01", LastVerified: "2020-06-01"},
			want:  false,
		},
		{
			name:  "expiry today, no last verified",
			entry: Entry{ExpiresAt: today},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.entry.IsStale()
			if got != tt.want {
				t.Errorf("IsStale() = %v, want %v", got, tt.want)
			}
		})
	}
}
