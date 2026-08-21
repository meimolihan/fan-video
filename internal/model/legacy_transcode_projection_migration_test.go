package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestFreshProfilesDoNotCreateLegacyTranscodeTasks(t *testing.T) {
	for _, migrate := range []struct {
		name string
		run  func(*gorm.DB) error
	}{
		{name: "lite", run: AutoMigrateLite},
		{name: "full", run: AutoMigrate},
	} {
		t.Run(migrate.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+migrate.name+"-no-legacy?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := migrate.run(db); err != nil {
				t.Fatal(err)
			}
			if db.Migrator().HasTable(&TranscodeTask{}) {
				t.Fatal("fresh profile created transcode_tasks")
			}
		})
	}
}

func TestExistingLegacyTranscodeTasksSurviveProfileMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:existing-legacy?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&TranscodeTask{}); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrateLite(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable(&TranscodeTask{}) {
		t.Fatal("existing legacy table was removed")
	}
}
