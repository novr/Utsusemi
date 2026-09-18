package provider

import (
	"fmt"
	"testing"
)

func TestIsBenignMissing(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("delete busy"), false},
		{fmt.Errorf("VM \"%s\" does not exist", "x"), true},
		{fmt.Errorf("Not Found"), false},
		{fmt.Errorf("no such file or directory"), true},
	}
	for _, tc := range cases {
		if got := IsBenignMissing(tc.err); got != tc.want {
			t.Fatalf("IsBenignMissing(%v)=%v, want %v", tc.err, got, tc.want)
		}
	}
}
