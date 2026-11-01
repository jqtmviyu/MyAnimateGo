package migration_test

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"github.com/jqtmviyu/MyAnimeGo/backend/internal/migration"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/pipeline"
	"github.com/jqtmviyu/MyAnimeGo/backend/internal/storage"
)

func TestTransmissionConnectionAndLegacyPathsAreConverted(t *testing.T) {
	source := fixtureDirectory(t, "v1.1.50")
	configPath := filepath.Join(source, "config.json")
	var config map[string]any
	readJSON(t, configPath, &config)
	config["download"] = "Transmission"
	config["host"] = "synthetic.invalid"
	config["username"] = "fixture-user"
	config["password"] = "fixture-password"
	config["downloadPath"] = "/synthetic/downloads"
	config["ovaDownloadPath"] = "/synthetic/ova"
	config["mikanHost"] = "synthetic-mikan.invalid"
	writeJSON(t, configPath, config)

	report, err := migration.Preview(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.PendingFields) != 0 {
		t.Fatalf("unconverted fields: %v", report.PendingFields)
	}
	if len(report.UnknownFields) != 0 {
		t.Fatalf("declared historical fields were reported unknown: %v", report.UnknownFields)
	}

	store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := migration.Import(context.Background(), store, source); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"downloadToolHost":        "synthetic.invalid",
		"downloadToolUsername":    "fixture-user",
		"downloadToolPassword":    "fixture-password",
		"downloadPath":            "/synthetic/downloads",
		"ovaDownloadPath":         "/synthetic/ova",
		"downloadPathTemplate":    "/synthetic/downloads/${title}",
		"ovaDownloadPathTemplate": "/synthetic/ova/${title}",
	} {
		if got[key] != want {
			t.Errorf("%s = %#v, want %#v", key, got[key], want)
		}
	}
}

func TestVersion12NestedStandbyAliasMatchesLegacyFallbackOrder(t *testing.T) {
	source := fixtureDirectory(t, "v1.2.82")
	subsPath := filepath.Join(source, "ani.json")
	var subscriptions []map[string]any
	readJSON(t, subsPath, &subscriptions)
	subscriptions[0]["offset"] = float64(3)
	subscriptions[0]["backRssList"] = []any{}
	subscriptions[0]["backRss"] = []any{"https://example.invalid/rss/legacy.xml"}
	writeJSON(t, subsPath, subscriptions)

	store, err := storage.Open(filepath.Join(t.TempDir(), "ani-rss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := migration.Import(context.Background(), store, source); err != nil {
		t.Fatal(err)
	}
	got, err := store.ListSubscriptions(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("imported subscriptions = %#v, %v", got, err)
	}
	standby := got[0]["standbyRssList"].([]any)[0].(map[string]any)
	if standby["url"] != "https://example.invalid/rss/legacy.xml" || standby["label"] != "未知字幕组" || standby["offset"] != float64(3) {
		t.Fatalf("legacy nested fallback changed: %#v", standby)
	}
}

func TestInventoryFieldsAreNotReportedUnknown(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "..", "docs", "migration", "model-fields.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	fields := make(map[string]map[string]map[string]bool)
	for _, row := range rows[1:] {
		version, model, field := row[0], row[2], row[3]
		if fields[version] == nil {
			fields[version] = make(map[string]map[string]bool)
		}
		if fields[version][model] == nil {
			fields[version][model] = make(map[string]bool)
		}
		fields[version][model][field] = true
	}
	versions := map[string]string{"1.1.50": "v1.1.50", "1.2.82": "v1.2.82", "3.2.52": "v3.2.52"}
	for version, fixture := range versions {
		t.Run(version, func(t *testing.T) {
			source := fixtureDirectory(t, fixture)
			config := fields[version]["Config"]
			configJSON := make(map[string]any, len(config))
			for field := range config {
				configJSON[field] = true
			}
			if version != "3.2.52" {
				configJSON["download"] = "Transmission"
			}
			configName := "config.json"
			subsName := "ani.json"
			if version == "3.2.52" {
				configName = "config.v2.json"
				subsName = "ani.v2.json"
			}
			writeJSON(t, filepath.Join(source, configName), configJSON)
			subJSON := make(map[string]any, len(fields[version]["Ani"]))
			for field := range fields[version]["Ani"] {
				subJSON[field] = true
			}
			subJSON["id"] = "synthetic-id"
			writeJSON(t, filepath.Join(source, subsName), []map[string]any{subJSON})
			report, err := migration.Preview(source)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.UnknownFields) != 0 {
				t.Fatalf("known fields reported unknown: %v", report.UnknownFields)
			}
		})
	}
}

func TestImportedLegacyPathsReachDownloadPipeline(t *testing.T) {
	for _, tc := range []struct {
		name, version, seasonName, want                         string
		acronym, quarter, merge, year, ova, custom, ovaFallback bool
	}{
		{name: "normal", version: "v1.1.50", seasonName: "Season 1", want: "tv/Show/Season 2"},
		{name: "padded-season", version: "v1.2.82", seasonName: "Season 01", want: "tv/Show/Season 02"},
		{name: "short-season", version: "v1.2.82", seasonName: "S1", want: "tv/Show/S2"},
		{name: "letter-precedes-quarter", version: "v1.2.82", seasonName: "S01", acronym: true, quarter: true, want: "tv/S/Show/S02"},
		{name: "month", version: "v1.1.50", quarter: true, want: "tv/2025-12/Show"},
		{name: "merged-quarter", version: "v1.2.82", quarter: true, merge: true, want: "tv/2025-10/Show"},
		{name: "year", version: "v1.2.82", year: true, want: "tv/2025/Show"},
		{name: "ova-root-fallback", version: "v1.2.82", seasonName: "S01", ova: true, ovaFallback: true, want: "tv/Show"},
		{name: "ova", version: "v1.2.82", seasonName: "S01", ova: true, want: "ova/Show"},
		{name: "custom", version: "v1.2.82", custom: true, want: "custom/Show"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fixtureDirectory(t, tc.version)
			root := t.TempDir()
			var config map[string]any
			readJSON(t, filepath.Join(source, "config.json"), &config)
			config["downloadPath"] = filepath.Join(root, "tv")
			config["ovaDownloadPath"] = filepath.Join(root, "ova")
			if tc.ovaFallback {
				config["ovaDownloadPath"] = ""
			}
			config["seasonName"] = tc.seasonName
			config["acronym"] = tc.acronym
			config["quarter"] = tc.quarter
			if tc.version == "v1.2.82" {
				config["quarterMerge"] = tc.merge
				config["yearStorage"] = tc.year
			}
			writeJSON(t, filepath.Join(source, "config.json"), config)
			sub := map[string]any{"id": "legacy-path", "title": "Show", "season": 2, "year": 2025, "month": 12, "ova": tc.ova, "customDownloadPath": tc.custom, "downloadPath": filepath.Join(root, "custom", "Show")}
			writeJSON(t, filepath.Join(source, "ani.json"), []map[string]any{sub})
			store, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			report, err := migration.Import(context.Background(), store, source)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.PendingFields) != 0 {
				t.Fatalf("pending fields: %v", report.PendingFields)
			}
			subs, err := store.ListSubscriptions(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			engine, err := pipeline.New(store, pipeline.Options{})
			if err != nil {
				t.Fatal(err)
			}
			got, err := engine.DownloadPath(context.Background(), subs[0])
			if err != nil {
				t.Fatal(err)
			}
			if got["downloadPath"] != filepath.Join(root, filepath.FromSlash(tc.want)) {
				t.Fatalf("path = %v, want %s", got["downloadPath"], filepath.Join(root, tc.want))
			}
		})
	}
}
