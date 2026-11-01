package rules

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

type baseline struct {
	RenameCases []struct {
		Name               string   `json:"name"`
		SubscriptionTitle  string   `json:"subscriptionTitle"`
		Season             int      `json:"season"`
		Offset             int      `json:"offset"`
		OVA                bool     `json:"ova"`
		CustomEpisode      bool     `json:"customEpisode"`
		CustomEpisodeRegex *string  `json:"customEpisodeRegex"`
		CustomEpisodeGroup *int     `json:"customEpisodeGroup"`
		SkipHalfEpisodes   bool     `json:"skipHalfEpisodes"`
		Subgroup           string   `json:"subgroup"`
		ItemTitle          string   `json:"itemTitle"`
		RenameTemplate     string   `json:"renameTemplate"`
		ExpectedAccepted   bool     `json:"expectedAccepted"`
		ExpectedEpisode    *float64 `json:"expectedEpisode"`
		ExpectedName       string   `json:"expectedName"`
	} `json:"renameCases"`
	FilenameReplacementCase struct {
		Input    string `json:"input"`
		Expected string `json:"expected"`
	} `json:"filenameReplacementCase"`
	RSSProgressCase struct {
		DownloadNew bool      `json:"downloadNew"`
		Episodes    []float64 `json:"episodes"`
		Expected    int       `json:"expectedCurrentEpisodeNumber"`
	} `json:"rssProgressCase"`
	RSSMissingEpisodeCase struct {
		Episodes []float64 `json:"episodes"`
		Expected []int     `json:"expectedMissingEpisodes"`
	} `json:"rssMissingEpisodeCase"`
}

func readBaseline(t *testing.T) baseline {
	t.Helper()
	data, err := os.ReadFile("testdata/behavior-baseline.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture baseline
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestRenameMatchesJavaBehaviorBaseline(t *testing.T) {
	fixture := readBaseline(t)
	for _, tc := range fixture.RenameCases {
		t.Run(tc.Name, func(t *testing.T) {
			item := Item{Title: tc.ItemTitle, Name: tc.ItemTitle, Subgroup: tc.Subgroup}
			input := RenameInput{Title: tc.SubscriptionTitle, Season: tc.Season, Offset: tc.Offset, OVA: tc.OVA, CustomEpisode: tc.CustomEpisode, CustomEpisodeRegex: tc.CustomEpisodeRegex, CustomEpisodeGroup: tc.CustomEpisodeGroup, SkipHalfEpisodes: tc.SkipHalfEpisodes, Template: tc.RenameTemplate}
			got, accepted, err := Rename(input, item)
			if err != nil {
				t.Fatalf("Rename returned error: %v", err)
			}
			if accepted != tc.ExpectedAccepted {
				t.Fatalf("accepted = %v, want %v", accepted, tc.ExpectedAccepted)
			}
			if tc.ExpectedEpisode != nil && (got.Episode == nil || *got.Episode != *tc.ExpectedEpisode) {
				t.Fatalf("episode = %v, want %v", got.Episode, *tc.ExpectedEpisode)
			}
			if tc.ExpectedEpisode == nil && got.Episode != nil {
				t.Fatalf("episode = %v, want nil", *got.Episode)
			}
			if got.Name != tc.ExpectedName {
				t.Fatalf("name = %q, want %q", got.Name, tc.ExpectedName)
			}
		})
	}
}

func TestFilenameReplacementMatchesJavaBehaviorBaseline(t *testing.T) {
	fixture := readBaseline(t)
	if got := SanitizeFilename(fixture.FilenameReplacementCase.Input); got != fixture.FilenameReplacementCase.Expected {
		t.Fatalf("SanitizeFilename() = %q, want %q", got, fixture.FilenameReplacementCase.Expected)
	}
}

func TestProgressAndMissingEpisodesMatchJavaBehaviorBaseline(t *testing.T) {
	fixture := readBaseline(t)
	episodes := fixture.RSSProgressCase.Episodes
	got := CurrentEpisodeNumber(ProgressOptions{Episodes: episodes, DownloadNew: fixture.RSSProgressCase.DownloadNew})
	if got != fixture.RSSProgressCase.Expected {
		t.Fatalf("CurrentEpisodeNumber() = %d, want %d", got, fixture.RSSProgressCase.Expected)
	}
	gotMissing := MissingEpisodes(MissingOptions{Episodes: fixture.RSSMissingEpisodeCase.Episodes, Enabled: true, SubscriptionEnabled: true})
	if !reflect.DeepEqual(gotMissing, fixture.RSSMissingEpisodeCase.Expected) {
		t.Fatalf("MissingEpisodes() = %v, want %v", gotMissing, fixture.RSSMissingEpisodeCase.Expected)
	}
}

func TestCustomEpisodeRejectsUnsupportedRegexp(t *testing.T) {
	pattern := `(?<=EP)\d+`
	_, _, err := Rename(RenameInput{CustomEpisode: true, CustomEpisodeRegex: &pattern, CustomEpisodeGroup: intPtr(0)}, Item{Title: "EP03"})
	if err == nil {
		t.Fatal("expected unsupported lookbehind to return a clear error")
	}
}

func TestCustomEpisodeRejectsMissingCaptureGroup(t *testing.T) {
	pattern := `EP\d+`
	group := 1
	_, _, err := Rename(RenameInput{CustomEpisode: true, CustomEpisodeRegex: &pattern, CustomEpisodeGroup: &group}, Item{Title: "EP03"})
	if err == nil {
		t.Fatal("expected an out-of-range capture group to return a clear error")
	}
}

func TestSourceMergePreservesPrimarySourcePriority(t *testing.T) {
	primary := []Item{{Title: "primary", Episode: floatPtr(1), Name: "same"}}
	standby := []Item{{Title: "standby", Episode: floatPtr(1), Name: "other"}, {Title: "next", Episode: floatPtr(2), Name: "next"}}
	got := MergeSources([][]Item{primary, standby}, false)
	if len(got) != 2 || got[0].Title != "primary" || got[1].Title != "next" {
		t.Fatalf("MergeSources() = %#v", got)
	}

	primary[0].Name = "same-name"
	standby = []Item{{Title: "standby duplicate", Episode: floatPtr(2), Name: "same-name"}, {Title: "standby unique", Episode: floatPtr(3), Name: "unique-name"}}
	got = MergeSources([][]Item{primary, standby}, true)
	if len(got) != 2 || got[0].Title != "primary" || got[1].Title != "standby unique" {
		t.Fatalf("MergeSources() coexist = %#v", got)
	}
}

func TestRenameRejectsTemplatePlaceholdersThatNeedMetadata(t *testing.T) {
	placeholders := []string{"${tmdbid}", "${episodeTitle}", "${bgmEpisodeTitle}", "${bgmJpEpisodeTitle}", "${bgmId}", "${jpTitle}", "${themoviedbName}"}
	for _, placeholder := range placeholders {
		t.Run(placeholder, func(t *testing.T) {
			item, accepted, err := Rename(RenameInput{Title: "Show", Season: 1, Template: "${title} " + placeholder}, Item{Title: "Show EP01"})
			if err == nil || !strings.Contains(err.Error(), placeholder) {
				t.Fatalf("Rename() error = %v, want unsupported placeholder %s", err, placeholder)
			}
			if accepted {
				t.Fatalf("Rename() accepted unsupported metadata template: %#v", item)
			}
		})
	}
}

func TestProgressSupportsLatestEpisodeAndPrimaryOnlyModes(t *testing.T) {
	got := CurrentEpisodeNumber(ProgressOptions{Episodes: []float64{1, 2.5, 4, 7}, DownloadNew: true, StandbyRSS: true, Coexist: true, MasterFlags: []bool{true, true, false, true}})
	if got != 7 {
		t.Fatalf("CurrentEpisodeNumber() = %d, want 7", got)
	}
	got = CurrentEpisodeNumber(ProgressOptions{Episodes: []float64{1, 2, 4, 7}, DownloadNew: true, StandbyRSS: true, Coexist: true, MasterFlags: []bool{true, true, false, false}})
	if got != 2 {
		t.Fatalf("CurrentEpisodeNumber() primary only = %d, want 2", got)
	}
}

func TestRulesKeepUnicodeTextIntact(t *testing.T) {
	got := SanitizeFilename("番組🎬:第１話")
	want := "番組🎬：第１話"
	if got != want {
		t.Fatalf("SanitizeFilename() = %q, want %q", got, want)
	}
}

func TestRenameUsesCallerProvidedMetadata(t *testing.T) {
	metadata := map[string]string{
		"tmdbid": "42", "episodeTitle": "First Airing", "bgmEpisodeTitle": "首播",
		"bgmJpEpisodeTitle": "初回", "bgmId": "bgm-9", "jpTitle": "日本語名", "themoviedbName": "TMDB 名",
	}
	got, accepted, err := Rename(RenameInput{Title: "Show", Season: 1, Template: "${title} ${tmdbid} ${episodeTitle} ${bgmEpisodeTitle} ${bgmJpEpisodeTitle} ${bgmId} ${jpTitle} ${themoviedbName}", Metadata: metadata}, Item{Title: "Show EP01"})
	if err != nil || !accepted {
		t.Fatalf("Rename() accepted=%v err=%v", accepted, err)
	}
	want := "Show 42 First Airing 首播 初回 bgm-9 日本語名 TMDB 名"
	if got.Name != want {
		t.Fatalf("Name = %q, want %q", got.Name, want)
	}
}

func TestRenameReportsMissingMetadata(t *testing.T) {
	got, accepted, err := Rename(RenameInput{Title: "Show", Season: 1, Template: "${title} ${episodeTitle}"}, Item{Title: "Show EP01"})
	if err == nil || !strings.Contains(err.Error(), "episodeTitle") || accepted {
		t.Fatalf("Rename() = (%#v, %v, %v), want missing episodeTitle error", got, accepted, err)
	}
}

func TestRenameDelOptionsAndFilenameLength(t *testing.T) {
	stripYear, stripID := false, true
	got, accepted, err := Rename(RenameInput{Title: "节目 (2024) [tmdbid=9]", Season: 1, Template: "${title} ${episode}", RenameDelYear: &stripYear, RenameDelTmdbId: &stripID, MaxFileNameLength: 2}, Item{Title: "Show EP01"})
	if err != nil || !accepted {
		t.Fatalf("Rename() accepted=%v err=%v", accepted, err)
	}
	if got.Name != "节目" {
		t.Fatalf("Name = %q, want safe Unicode truncation %q", got.Name, "节目")
	}

	got, accepted, err = Rename(RenameInput{Title: "A🎬B", Season: 1, Template: "${title}", MaxFileNameLength: 2}, Item{Title: "Show EP01"})
	if err != nil || !accepted || got.Name != "A🎬" {
		t.Fatalf("supplementary Unicode truncation = (%q, %v, %v), want A🎬", got.Name, accepted, err)
	}
}

func TestOVARenameCleansConfiguredMetadataWithoutFilenameConversion(t *testing.T) {
	got, accepted, err := Rename(RenameInput{Title: "OVA/Title: (2024) [tmdbid=99]", OVA: true}, Item{Title: "ignored"})
	if err != nil || !accepted {
		t.Fatalf("Rename() accepted=%v err=%v", accepted, err)
	}
	if got.Name != "OVA/Title:" {
		t.Fatalf("OVA name = %q, want raw renameDel result %q", got.Name, "OVA/Title:")
	}
}

func TestRenameDelDefaultsAndCaseSensitivityMatchJava(t *testing.T) {
	got, accepted, err := Rename(RenameInput{Title: "Show (2024) [tmdbid=99]", OVA: true}, Item{})
	if err != nil || !accepted || got.Name != "Show" {
		t.Fatalf("default cleanup = (%q, %v, %v), want Show", got.Name, accepted, err)
	}
	keepYear, keepID := false, false
	got, accepted, err = Rename(RenameInput{Title: "Show (2024) [tmdbid=99]", OVA: true, RenameDelYear: &keepYear, RenameDelTmdbId: &keepID}, Item{})
	if err != nil || !accepted || got.Name != "Show (2024) [tmdbid=99]" {
		t.Fatalf("disabled cleanup = (%q, %v, %v), want original title", got.Name, accepted, err)
	}
	got, accepted, err = Rename(RenameInput{Title: "Show [TMDBID=99]", OVA: true}, Item{})
	if err != nil || !accepted || got.Name != "Show [TMDBID=99]" {
		t.Fatalf("case-sensitive cleanup = (%q, %v, %v), want uppercase id retained", got.Name, accepted, err)
	}
}

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }
