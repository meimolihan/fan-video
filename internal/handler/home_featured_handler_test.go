package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fan-video/fan-video/internal/model"
	"github.com/fan-video/fan-video/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func newHomeFeaturedTestEnv(t *testing.T) (*HomeFeaturedHandler, *repository.Repositories) {
	t.Helper()
	dbName := "test_home_featured_" + strings.ReplaceAll(t.Name(), "/", "_")
	db, err := gorm.Open(sqlite.Open("file:"+dbName+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.HomeFeatured{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.NewRepositories(db)

	if err := repos.Library.Create(&model.Library{ID: "lib-visible", Name: "可见库", Path: "/tmp/visible", Type: "movie"}); err != nil {
		t.Fatalf("create visible lib: %v", err)
	}
	if err := repos.Library.Create(&model.Library{ID: "lib-hidden", Name: "隐藏库", Path: "/tmp/hidden", Type: "movie", Hidden: true}); err != nil {
		t.Fatalf("create hidden lib: %v", err)
	}

	if err := repos.Media.Create(&model.Media{ID: "mv-1", LibraryID: "lib-visible", Title: "可见影片", MediaType: "movie", FilePath: "/tmp/visible/mv1.mkv"}); err != nil {
		t.Fatalf("create visible media: %v", err)
	}
	if err := repos.Media.Create(&model.Media{ID: "mh-1", LibraryID: "lib-hidden", Title: "隐藏影片", MediaType: "movie", FilePath: "/tmp/hidden/mh1.mkv"}); err != nil {
		t.Fatalf("create hidden media: %v", err)
	}

	if err := repos.HomeFeatured.Create(&model.HomeFeatured{ItemType: "movie", ItemID: "mv-1"}); err != nil {
		t.Fatalf("create featured visible: %v", err)
	}
	if err := repos.HomeFeatured.Create(&model.HomeFeatured{ItemType: "movie", ItemID: "mh-1"}); err != nil {
		t.Fatalf("create featured hidden: %v", err)
	}

	h := NewHomeFeaturedHandler(repos.HomeFeatured, repos.Media, repos.Series, repos.Library, zap.NewNop().Sugar())
	return h, repos
}

func TestHomeFeaturedListForHomeExcludesHiddenLibraries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newHomeFeaturedTestEnv(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/home/featured", nil)
	h.ListForHome(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		Data []struct {
			Type  string      `json:"type"`
			Media model.Media `json:"media"`
		} `json:"data"`
		Active bool `json:"active"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Data) != 1 {
		t.Fatalf("data 长度 = %d, 期望只保留可见库的 1 条（隐藏库条目应被过滤）: %s", len(resp.Data), w.Body.String())
	}
	if resp.Data[0].Media.ID != "mv-1" {
		t.Fatalf("返回了隐藏库内容: media id = %s", resp.Data[0].Media.ID)
	}
}

func TestHomeFeaturedReorderPreservesListOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, repos := newHomeFeaturedTestEnv(t)

	rows, err := repos.HomeFeatured.List()
	if err != nil || len(rows) != 2 {
		t.Fatalf("读取精选条目失败: rows=%v err=%v", rows, err)
	}

	// 原顺序：[mv-1, mh-1]，反向提交后 List 应按新顺序返回（忽略隐藏库过滤，直接看仓储层面）
	if err := repos.HomeFeatured.UpdateSortOrder(rows[0].ID, 2); err != nil {
		t.Fatalf("update sort order: %v", err)
	}
	if err := repos.HomeFeatured.UpdateSortOrder(rows[1].ID, 1); err != nil {
		t.Fatalf("update sort order: %v", err)
	}

	sorted, err := repos.HomeFeatured.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sorted) != 2 || sorted[0].ItemID != "mh-1" || sorted[1].ItemID != "mv-1" {
		t.Fatalf("排序未生效: %+v", sorted)
	}

	// Reorder 接口按提交顺序整体重排
	ids := []string{rows[0].ID, rows[1].ID}
	body, _ := json.Marshal(map[string][]string{"ids": ids})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/admin/home-featured/sort", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.Reorder(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	after, err := repos.HomeFeatured.List()
	if err != nil {
		t.Fatalf("list after reorder: %v", err)
	}
	if len(after) != 2 || after[0].ItemID != "mv-1" || after[1].ItemID != "mh-1" {
		t.Fatalf("重排后顺序未恢复为提交顺序: %+v", after)
	}
}