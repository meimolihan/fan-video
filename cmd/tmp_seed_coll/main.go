// tmp_seed_coll 一次性工具：向运行数据库植入测试合集数据（含隐藏库合集）。
// 用法: go run ./tmp_seed_coll <db-path> <visible-lib-id> <hidden-lib-id>
package main

import (
	"fmt"
	"os"

	"github.com/fan-video/fan-video/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: tmp_seed_coll <db> <visible-lib-id> <hidden-lib-id>")
		os.Exit(1)
	}
	dbPath, visLib, hidLib := os.Args[1], os.Args[2], os.Args[3]

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	must(err)

	colls := []model.MovieCollection{
		{ID: "tmpx-colls-visible", Name: "临时候集(可见)", MediaCount: 2},
		{ID: "tmpx-colls-hybrid", Name: "临时候集(混合)", MediaCount: 2},
		{ID: "tmpx-colls-hidden", Name: "临时候集(全隐藏)", MediaCount: 2},
	}
	for _, c := range colls {
		var existing int64
		must(db.Model(&model.MovieCollection{}).Where("id = ?", c.ID).Count(&existing).Error)
		if existing == 0 {
			must(db.Create(&c).Error)
			fmt.Println("created collection", c.ID, c.Name)
		}
	}

	medias := []model.Media{
		{ID: "tmpx-media-v1", LibraryID: visLib, CollectionID: "tmpx-colls-visible", Title: "临时候集(可见)甲", MediaType: "movie", FilePath: "/dev/null/v1.mkv"},
		{ID: "tmpx-media-v2", LibraryID: visLib, CollectionID: "tmpx-colls-visible", Title: "临时候集(可见)乙", MediaType: "movie", FilePath: "/dev/null/v2.mkv"},
		{ID: "tmpx-media-hy1", LibraryID: visLib, CollectionID: "tmpx-colls-hybrid", Title: "临时候集(混合)甲", MediaType: "movie", FilePath: "/dev/null/hy1.mkv"},
		{ID: "tmpx-media-hy2", LibraryID: hidLib, CollectionID: "tmpx-colls-hybrid", Title: "临时候集(混合)乙", MediaType: "movie", FilePath: "/dev/null/hy2.mkv"},
		{ID: "tmpx-media-h1", LibraryID: hidLib, CollectionID: "tmpx-colls-hidden", Title: "临时候集(全隐藏)甲", MediaType: "movie", FilePath: "/dev/null/h1.mkv"},
		{ID: "tmpx-media-h2", LibraryID: hidLib, CollectionID: "tmpx-colls-hidden", Title: "临时候集(全隐藏)乙", MediaType: "movie", FilePath: "/dev/null/h2.mkv"},
	}
	for _, m := range medias {
		var existing int64
		must(db.Model(&model.Media{}).Where("id = ?", m.ID).Count(&existing).Error)
		if existing == 0 {
			must(db.Create(&m).Error)
			fmt.Println("created media", m.ID)
		}
	}

	fmt.Println("seed done")
}