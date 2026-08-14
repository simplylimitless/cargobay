package search

import "testing"

func TestDispatch(t *testing.T) {
	cases := []struct {
		registryID string
		wantOK     bool
	}{
		{"dockerhub", true},
		{"quay", true},
		{"ghcr", false},
		{"npm", false},
		{"", false},
	}

	for _, c := range cases {
		_, ok := Dispatch(c.registryID)
		if ok != c.wantOK {
			t.Errorf("Dispatch(%q) ok = %v, want %v", c.registryID, ok, c.wantOK)
		}
	}
}
