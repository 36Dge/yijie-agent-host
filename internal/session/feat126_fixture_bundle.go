package session

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const FEAT126FixtureDatasetID = "feat126-title-raw-v1"

//go:embed testdata/feat126-title-raw-v1/manifest.json testdata/feat126-title-raw-v1/lock.json testdata/feat126-title-raw-v1/dataset.jsonl
var feat126FixtureFiles embed.FS

type FEAT126FakeFixture struct {
	ID                string
	RawScenario       string
	RawText           string
	ExpectedRawStatus string
	AssistantText     string
	DatasetSHA256     string
	ManifestSHA256    string
	LockSHA256        string
}

type feat126FixtureManifest struct {
	DatasetID string `json:"dataset_id"`
	Provider  string `json:"provider"`
}

type feat126FixtureLock struct {
	DatasetID            string            `json:"dataset_id"`
	FrozenBeforeFirstRun bool              `json:"frozen_before_first_run"`
	HashAlgorithm        string            `json:"hash_algorithm"`
	Files                map[string]string `json:"files"`
}

type feat126FixtureCase struct {
	ID                string `json:"id"`
	FakeTitleOutput   string `json:"fake_title_output"`
	RawScenario       string `json:"raw_scenario"`
	RawText           string `json:"raw_text"`
	ExpectedRawStatus string `json:"expected_raw_status"`
}

func LoadFEAT126FakeFixture(id string) (FEAT126FakeFixture, error) {
	if id == "" {
		return FEAT126FakeFixture{}, errors.New("fixture id is required")
	}
	manifestBytes, err := feat126FixtureFiles.ReadFile("testdata/feat126-title-raw-v1/manifest.json")
	if err != nil {
		return FEAT126FakeFixture{}, errors.New("fixture manifest is unavailable")
	}
	lockBytes, err := feat126FixtureFiles.ReadFile("testdata/feat126-title-raw-v1/lock.json")
	if err != nil {
		return FEAT126FakeFixture{}, errors.New("fixture lock is unavailable")
	}
	datasetBytes, err := feat126FixtureFiles.ReadFile("testdata/feat126-title-raw-v1/dataset.jsonl")
	if err != nil {
		return FEAT126FakeFixture{}, errors.New("fixture dataset is unavailable")
	}

	var manifest feat126FixtureManifest
	if json.Unmarshal(manifestBytes, &manifest) != nil || manifest.DatasetID != FEAT126FixtureDatasetID || manifest.Provider != "deterministic-fake" {
		return FEAT126FakeFixture{}, errors.New("fixture manifest identity is invalid")
	}
	var lock feat126FixtureLock
	if json.Unmarshal(lockBytes, &lock) != nil || lock.DatasetID != FEAT126FixtureDatasetID || !lock.FrozenBeforeFirstRun || lock.HashAlgorithm != "sha256" {
		return FEAT126FakeFixture{}, errors.New("fixture lock identity is invalid")
	}
	if err := verifyFEAT126FixtureDigest(lock.Files, "manifest.json", manifestBytes); err != nil {
		return FEAT126FakeFixture{}, err
	}
	if err := verifyFEAT126FixtureDigest(lock.Files, "dataset.jsonl", datasetBytes); err != nil {
		return FEAT126FakeFixture{}, err
	}

	var selected feat126FixtureCase
	scanner := bufio.NewScanner(bytes.NewReader(datasetBytes))
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var candidate feat126FixtureCase
		if json.Unmarshal(scanner.Bytes(), &candidate) != nil {
			return FEAT126FakeFixture{}, errors.New("fixture dataset contains invalid JSON")
		}
		if candidate.ID == id {
			selected = candidate
			break
		}
	}
	if scanner.Err() != nil {
		return FEAT126FakeFixture{}, errors.New("fixture dataset could not be read")
	}
	if selected.ID == "" || selected.RawText == "" || selected.FakeTitleOutput == "" {
		return FEAT126FakeFixture{}, errors.New("fixture id is absent or incomplete")
	}
	return FEAT126FakeFixture{
		ID:                selected.ID,
		RawScenario:       selected.RawScenario,
		RawText:           selected.RawText,
		ExpectedRawStatus: selected.ExpectedRawStatus,
		AssistantText:     selected.FakeTitleOutput,
		DatasetSHA256:     sha256Hex(datasetBytes),
		ManifestSHA256:    sha256Hex(manifestBytes),
		LockSHA256:        sha256Hex(lockBytes),
	}, nil
}

func verifyFEAT126FixtureDigest(files map[string]string, name string, content []byte) error {
	expected := files[name]
	if expected == "" || expected != sha256Hex(content) {
		return fmt.Errorf("fixture %s digest does not match the frozen lock", name)
	}
	return nil
}

func sha256Hex(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}
