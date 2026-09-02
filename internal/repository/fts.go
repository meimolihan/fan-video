package repository

import (
	"strings"

	"gorm.io/gorm"
)

// mediaFTSColumns FTS5 全文索引覆盖的可检索字段。
// tokenize=trigram 用 3 字符滑动窗口建索引，支持与 LIKE %kw% 等价的子串匹配
// （含中文：title 按字符窗口切分），避免 LIKE 全表扫描。
const mediaFTSColumns = "id UNINDEXED, title, orig_title, genres, tags, overview, episode_title, maker, publisher, label, num, sort_title, tagline, original_plot, outline"

// initMediaFTS 幂等地创建 media_fts 全文索引表与同步触发器，并回填数据。
// 当前端 SQLite 不支持 FTS5/trigram 时静默跳过（调用方回退到 LIKE 搜索）。
func initMediaFTS(db *gorm.DB) error {
	if err := db.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS media_fts USING fts5(" + mediaFTSColumns + ", tokenize='trigram')").Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE TRIGGER IF NOT EXISTS media_fts_ai AFTER INSERT ON media BEGIN INSERT INTO media_fts(rowid, id, title, orig_title, genres, tags, overview, episode_title, maker, publisher, label, num, sort_title, tagline, original_plot, outline) VALUES (new.rowid, new.id, new.title, new.orig_title, new.genres, new.tags, new.overview, new.episode_title, new.maker, new.publisher, new.label, new.num, new.sort_title, new.tagline, new.original_plot, new.outline); END").Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE TRIGGER IF NOT EXISTS media_fts_ad AFTER DELETE ON media BEGIN DELETE FROM media_fts WHERE rowid = old.rowid; END").Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE TRIGGER IF NOT EXISTS media_fts_au AFTER UPDATE ON media BEGIN DELETE FROM media_fts WHERE rowid = old.rowid; INSERT INTO media_fts(rowid, id, title, orig_title, genres, tags, overview, episode_title, maker, publisher, label, num, sort_title, tagline, original_plot, outline) VALUES (new.rowid, new.id, new.title, new.orig_title, new.genres, new.tags, new.overview, new.episode_title, new.maker, new.publisher, new.label, new.num, new.sort_title, new.tagline, new.original_plot, new.outline); END").Error; err != nil {
		return err
	}

	// 首次创建（索引为空）时从 media 回填，避免与触发器重复写入。
	var count int64
	if err := db.Raw("SELECT count(*) FROM media_fts").Scan(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		ins := "INSERT INTO media_fts(rowid, id, title, orig_title, genres, tags, overview, episode_title, maker, publisher, label, num, sort_title, tagline, original_plot, outline) " +
			"SELECT rowid, id, title, orig_title, genres, tags, overview, episode_title, maker, publisher, label, num, sort_title, tagline, original_plot, outline FROM media"
		if err := db.Exec(ins).Error; err != nil {
			return err
		}
	}
	return nil
}

// ftsPhrase 将用户关键字构造成安全的 FTS5 短语查询（trigram 下等价于子串匹配）。
func ftsPhrase(keyword string) string {
	return `"` + strings.ReplaceAll(keyword, `"`, `""`) + `"`
}