package repository

import (
	"testing"

	"github.com/fan-video/fan-video/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMediaFTSIndexAndSearch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := model.AutoMigrateLite(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := &MediaRepo{db: db}
	if err := initMediaFTS(db); err != nil {
		t.Skipf("FTS5/trigram 不可用，跳过: %v", err)
	}
	repo.ftsEnabled = true

	// 插入两条媒体（触发器应同步进入 media_fts）
	media := []*model.Media{
		{ID: "m1", LibraryID: "lib1", Title: "复仇者联盟：终局之战", FilePath: "/v/avengers.mp4", MediaType: "movie"},
		{ID: "m2", LibraryID: "lib1", Title: "Rogue One", FilePath: "/v/rogue.mp4", MediaType: "movie"},
		{ID: "m3", LibraryID: "lib2", Title: "复仇者联盟3", FilePath: "/v/avengers3.mp4", MediaType: "movie"},
	}
	for _, m := range media {
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	// 触发器同步校验
	var ftsCount int64
	if err := db.Raw("SELECT count(*) FROM media_fts").Scan(&ftsCount).Error; err != nil {
		t.Fatalf("count fts: %v", err)
	}
	if ftsCount != 3 {
		t.Fatalf("期望 media_fts 有 3 条记录，实际 %d", ftsCount)
	}

	// 中文子串搜索（trigram）
	hit, total, err := repo.Search("复仇者", 1, 20)
	if err != nil {
		t.Fatalf("search FTS: %v", err)
	}
	if total != 2 || len(hit) != 2 {
		t.Fatalf("中文搜索期望 2 条，实际 total=%d len=%d", total, len(hit))
	}

	// 英文搜索
	hit, total, err = repo.Search("rogue", 1, 20)
	if err != nil {
		t.Fatalf("search FTS: %v", err)
	}
	if total != 1 || len(hit) != 1 || hit[0].ID != "m2" {
		t.Fatalf("英文搜索期望 m2，实际 total=%d len=%d", total, len(hit))
	}

	// 排除库过滤
	_, total, err = repo.Search("复仇者", 1, 20, "lib1")
	if err != nil {
		t.Fatalf("search with exclude: %v", err)
	}
	if total != 1 {
		t.Fatalf("排除 lib1 后期望 1 条，实际 %d", total)
	}

	// 更新标题后触发器应更新索引
	if err := db.Model(media[0]).Update("title", "复仇者联盟4").Error; err != nil {
		t.Fatalf("update media: %v", err)
	}
	if _, total, err := repo.Search("终局之战", 1, 20); err == nil && total != 0 {
		t.Fatalf("更新后旧标题不应命中，实际 total=%d", total)
	}
	if _, total, err := repo.Search("复仇者联盟4", 1, 20); err != nil || total != 1 {
		t.Fatalf("更新后新标题应命中，实际 total=%d err=%v", total, err)
	}

	// 删除后触发器应移除索引
	if err := db.Delete(media[2]).Error; err != nil {
		t.Fatalf("delete media: %v", err)
	}
	if _, total, err := repo.Search("复仇者联盟3", 1, 20); err != nil || total != 0 {
		t.Fatalf("删除后不应命中，实际 total=%d err=%v", total, err)
	}
}