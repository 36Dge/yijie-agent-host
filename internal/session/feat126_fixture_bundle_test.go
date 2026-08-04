package session

import "testing"

func TestLoadFEAT126FakeFixtureUsesFrozenAuthority(t *testing.T) {
	fixture, err := LoadFEAT126FakeFixture("normal-000")
	if err != nil {
		t.Fatal(err)
	}
	if fixture.ID != "normal-000" || fixture.RawScenario != "complete" || fixture.ExpectedRawStatus != "complete" {
		t.Fatalf("unexpected fixture identity: %#v", fixture)
	}
	if fixture.DatasetSHA256 != "523609b44fd244fff18b930c992375999276c2e0d5786efadfd8858ec623b308" ||
		fixture.ManifestSHA256 != "7196ede3defe1b34e7f9c2cc2e869dedf887206112ace31d94f3cf87fa5f2f2c" {
		t.Fatalf("fixture authority drifted: %#v", fixture)
	}
	if _, err := LoadFEAT126FakeFixture("unknown"); err == nil {
		t.Fatal("unknown fixture id was accepted")
	}
}
