package serverprofile

import (
	"testing"

	"github.com/fan-video/fan-video/internal/config"
)

func TestLiteCoreAndUnsupportedCapabilities(t *testing.T) {
	cfg := &config.Config{}
	manifest := Lite(cfg)

	if manifest.Profile != "fan-video" {
		t.Fatalf("expected fan-video profile, got %q", manifest.Profile)
	}
	if manifest.SchemaVersion != SchemaVersion {
		t.Fatalf("expected schema version %d, got %d", SchemaVersion, manifest.SchemaVersion)
	}

	for _, name := range []string{"library", "playback", "transcode", "metadata", "subtitles", "task_center"} {
		capability := manifest.Capabilities[name]
		if !capability.Available || !capability.Enabled || !capability.Configured {
			t.Fatalf("core capability %q must be available, configured and enabled: %+v", name, capability)
		}
	}

	for _, name := range []string{"preprocess", "adult_scraper", "cast", "music", "photos", "federation", "plugins"} {
		capability := manifest.Capabilities[name]
		if capability.Available || capability.Enabled || capability.Configured {
			t.Fatalf("lite-only exclusion %q must be unavailable: %+v", name, capability)
		}
	}
	if _, exists := manifest.Capabilities["pulse"]; exists {
		t.Fatal("Pulse must not remain in the Lite capability contract")
	}
}

func TestLiteOptionalCapabilitiesFollowConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.WebDAV.Enabled = true
	cfg.Storage.Alist.Enabled = true
	cfg.Storage.S3.Enabled = false

	manifest := Lite(cfg)
	if !manifest.Capabilities["webdav"].Enabled || !manifest.Capabilities["alist"].Enabled {
		t.Fatal("configured remote storage capabilities should be enabled")
	}
	if manifest.Capabilities["s3"].Enabled || manifest.Capabilities["s3"].Configured {
		t.Fatal("disabled S3 capability must remain disabled")
	}

	legacy := manifest.LegacyFeatures(cfg)
	if legacy["profile"] != "fan-video" {
		t.Fatalf("legacy feature profile mismatch: %#v", legacy["profile"])
	}
	if legacy["webdav"] != true || legacy["alist"] != true {
		t.Fatalf("legacy remote storage flags mismatch: %#v", legacy)
	}
	if legacy["s3"] != false {
		t.Fatalf("disabled S3 legacy flag must be false: %#v", legacy)
	}
}

func TestFullManifestExposesAdvancedCapabilities(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.WebDAV.Enabled = true

	manifest := Full(cfg)
	if manifest.Profile != "full" || manifest.SchemaVersion != SchemaVersion {
		t.Fatalf("unexpected full manifest identity: %+v", manifest)
	}

	for _, name := range []string{"preprocess", "subtitle_preprocess", "cast", "music", "photos", "federation", "plugins", "offline_download", "user_profiles", "comments", "danmaku"} {
		capability := manifest.Capabilities[name]
		if !capability.Available || !capability.Enabled || !capability.Configured {
			t.Fatalf("full capability %q must be available and enabled: %+v", name, capability)
		}
	}

	if manifest.Capabilities["task_center"].Available {
		t.Fatal("full keeps its advanced task pages instead of the Lite task center")
	}
	if _, exists := manifest.Capabilities["pulse"]; exists {
		t.Fatal("Pulse must not remain in the Full capability contract")
	}
	if !manifest.Capabilities["webdav"].Enabled {
		t.Fatal("configured Full WebDAV must be enabled")
	}
}
