package service

import (
	"strings"
	"testing"

	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func newCollectionHiddenTestEnv(t *testing.T) *CollectionService {
	t.Helper()
	dbName := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.MovieCollection{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.NewRepositories(db)

	libs := []model.Library{
		{ID: "lib-visible", Name: "可见库", Path: "/tmp/visible", Type: "movie"},
		{ID: "lib-hidden", Name: "隐藏库", Path: "/tmp/hidden", Type: "movie", Hidden: true},
	}
	for _, l := range libs {
		if err := repos.Library.Create(&l); err != nil {
			t.Fatalf("create lib %s: %v", l.Name, err)
		}
	}

	medias := []model.Media{
		{ID: "cv-a", LibraryID: "lib-visible", CollectionID: "coll-visible", Title: "合集X 影片A", MediaType: "movie", FilePath: "/tmp/v/a.mkv"},
		{ID: "cv-b", LibraryID: "lib-visible", CollectionID: "coll-visible", Title: "合集X 影片B", MediaType: "movie", FilePath: "/tmp/v/b.mkv"},
		{ID: "ch-a", LibraryID: "lib-hidden", CollectionID: "coll-hidden", Title: "合集Y 影片A", MediaType: "movie", FilePath: "/tmp/h/a.mkv"},
		{ID: "ch-b", LibraryID: "lib-hidden", CollectionID: "coll-hidden", Title: "合集Y 影片B", MediaType: "movie", FilePath: "/tmp/h/b.mkv"},
		{ID: "cm-a", LibraryID: "lib-visible", CollectionID: "coll-mixed", Title: "合集Z 影片A", MediaType: "movie", FilePath: "/tmp/v/c.mkv"},
		{ID: "cm-b", LibraryID: "lib-hidden", CollectionID: "coll-mixed", Title: "合集Z 影片B", MediaType: "movie", FilePath: "/tmp/h/c.mkv"},
	}
	for _, m := range medias {
		if err := repos.Media.Create(&m); err != nil {
			t.Fatalf("create media %s: %v", m.ID, err)
		}
	}

	colls := []model.MovieCollection{
		{ID: "coll-visible", Name: "合集X", MediaCount: 2},
		{ID: "coll-hidden", Name: "合集Y", MediaCount: 2},
		{ID: "coll-mixed", Name: "合集Z", MediaCount: 2},
	}
	for _, c := range colls {
		if err := db.Create(&c).Error; err != nil {
			t.Fatalf("create coll %s: %v", c.ID, err)
		}
	}

	svc := NewCollectionService(repos.MovieCollection, repos.Media, zap.NewNop().Sugar())
	svc.SetLibraryRepo(repos.Library)
	return svc
}

func TestListCollectionsWithOptionsExcludesFullyHiddenCollections(t *testing.T) {
	svc := newCollectionHiddenTestEnv(t)

	colls, total, err := svc.ListCollectionsWithOptions(1, 20, "name_asc", "", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if total != 2 {
		t.Fatalf("total = %d, 期望 2（仅可见库合集和混合合集，全隐藏库合集应被排除）", total)
	}
	got := map[string]bool{}
	for _, c := range colls {
		got[c.ID] = true
	}
	if !got["coll-visible"] {
		t.Errorf("可见库合集 coll-visible 未返回")
	}
	if !got["coll-mixed"] {
		t.Errorf("混合合集 coll-mixed 未返回")
	}
	if got["coll-hidden"] {
		t.Errorf("全隐藏库合集 coll-hidden 不应出现在列表中")
	}
}

func TestListCollectionsWithOptionsExplicitLibraryIDKeepsHidden(t *testing.T) {
	svc := newCollectionHiddenTestEnv(t)

	colls, total, err := svc.ListCollectionsWithOptions(1, 20, "name_asc", "", "lib-hidden")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if total != 2 {
		t.Fatalf("total = %d, 期望 2（显式指定隐藏库时不排除该库合集）", total)
	}
	got := map[string]bool{}
	for _, c := range colls {
		got[c.ID] = true
	}
	if !got["coll-hidden"] || !got["coll-mixed"] {
		t.Fatalf("应返回 coll-hidden 和 coll-mixed，实际得到 %d 条", len(colls))
	}
}

func TestSearchCollectionsExcludesFullyHiddenCollections(t *testing.T) {
	svc := newCollectionHiddenTestEnv(t)

	colls, err := svc.SearchCollections("合集", 20)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	got := map[string]bool{}
	for _, c := range colls {
		got[c.ID] = true
	}
	if !got["coll-visible"] {
		t.Errorf("搜索未返回 coll-visible")
	}
	if !got["coll-mixed"] {
		t.Errorf("搜索未返回 coll-mixed")
	}
	if got["coll-hidden"] {
		t.Errorf("搜索返回了全隐藏库合集 coll-hidden")
	}
}

func TestGetCollectionDetailFiltersHiddenMedia(t *testing.T) {
	svc := newCollectionHiddenTestEnv(t)

	mixed, err := svc.GetCollectionDetail("coll-mixed")
	if err != nil {
		t.Fatalf("detail mixed: %v", err)
	}
	if len(mixed.Media) != 1 || mixed.Media[0].ID != "cm-a" {
		t.Fatalf("混合合集应只返回可见库影片，得到 %d 条: ids=%v", len(mixed.Media), mediaIDs(mixed))
	}

	hidden, err := svc.GetCollectionDetail("coll-hidden")
	if err != nil {
		t.Fatalf("detail hidden: %v", err)
	}
	if len(hidden.Media) != 0 {
		t.Fatalf("全隐藏库合集详情不应返回影片，得到 %d 条", len(hidden.Media))
	}
}

func TestGetCollectionByMediaIDFiltersHiddenMedia(t *testing.T) {
	svc := newCollectionHiddenTestEnv(t)

	res, err := svc.GetCollectionByMediaID("cm-b")
	if err != nil {
		t.Fatalf("by media: %v", err)
	}
	if len(res.Media) != 1 || res.Media[0].ID != "cm-a" {
		t.Fatalf("基于隐藏影片查询合集应只返回可见影片，得到 ids=%v", mediaIDs(res))
	}
	if res.Media[0].IsCurrent {
		t.Errorf("可见影片不应标记为当前影片")
	}
}

func mediaIDs(r *CollectionWithMedia) []string {
	ids := make([]string, 0, len(r.Media))
	for _, m := range r.Media {
		ids = append(ids, m.ID)
	}
	return ids
}