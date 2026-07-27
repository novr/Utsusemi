package provider

import "testing"

func TestParseTartLocalList(t *testing.T) {
	input := []byte(`[
		{"Name":"utsusemi-abcd","State":"running"},
		{"Name":"other-vm","State":"stopped"}
	]`)
	vms, err := parseTartLocalList(input, "utsusemi-")
	if err != nil {
		t.Fatal(err)
	}
	if len(vms) != 1 || vms[0].Name != "utsusemi-abcd" || !vms[0].Running {
		t.Fatalf("unexpected vms: %+v", vms)
	}
}
