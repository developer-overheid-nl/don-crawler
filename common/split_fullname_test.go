package common

import "testing"

func TestSplitFullName(t *testing.T) {
	tests := []struct {
		name       string
		fullName   string
		wantVendor string
		wantRepo   string
	}{
		{
			name:       "owner repo",
			fullName:   "owner/repo",
			wantVendor: "owner",
			wantRepo:   "repo",
		},
		{
			name:       "nested namespace",
			fullName:   "group/subgroup/repo",
			wantVendor: "group/subgroup",
			wantRepo:   "repo",
		},
		{
			name:       "single segment",
			fullName:   "repo",
			wantVendor: "",
			wantRepo:   "repo",
		},
		{
			name:       "empty",
			fullName:   "",
			wantVendor: "",
			wantRepo:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vendor, repo := SplitFullName(tc.fullName)
			if vendor != tc.wantVendor || repo != tc.wantRepo {
				t.Fatalf("SplitFullName(%q) = (%q, %q), want (%q, %q)", tc.fullName, vendor, repo, tc.wantVendor, tc.wantRepo)
			}
		})
	}
}
