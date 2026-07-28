package target

import "testing"

func TestFromConfigOrg(t *testing.T) {
	tgt, err := FromConfig(ConfigYAML{Org: "my-org", RunnerGroupID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Type != TypeOrg || tgt.Org != "my-org" || tgt.RunnerGroupID != 1 {
		t.Fatalf("unexpected target: %+v", tgt)
	}
}

func TestFromConfigOrgNormalizesCase(t *testing.T) {
	tgt, err := FromConfig(ConfigYAML{Org: "My-Org", RunnerGroupID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Org != "my-org" {
		t.Fatalf("org=%q", tgt.Org)
	}
}

func TestFromConfigRepo(t *testing.T) {
	tgt, err := FromConfig(ConfigYAML{Repo: "alice/my-app"})
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Type != TypeRepo || tgt.Owner != "alice" || tgt.Repo != "my-app" {
		t.Fatalf("unexpected target: %+v", tgt)
	}
}

func TestFromConfigMutuallyExclusive(t *testing.T) {
	_, err := FromConfig(ConfigYAML{Org: "o", Repo: "a/r", RunnerGroupID: 1})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFromConfigOrgRequiresRunnerGroup(t *testing.T) {
	_, err := FromConfig(ConfigYAML{Org: "my-org"})
	if err == nil {
		t.Fatal("expected error")
	}
}
