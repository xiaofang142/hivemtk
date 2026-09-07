package main

import (
	"fmt"
	"hivemtk-user/internal/geo/model"
	

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
)

func main() {
	dsn := os.Getenv("GEO_SEED_DSN")
	if dsn == "" {
		panic("GEO_SEED_DSN required")
	}
	g, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		panic(err)
	}
	kws := []string{"私有化部署SCRM推荐", "开源SCRM哪个好", "企微SCRM对比", "AI获客工具", "私域营销系统"}
	for _, k := range kws {
		if err := g.Create(&model.GeoKeyword{Keyword: k, Source: "manual", Intent: "信息", Status: "active"}).Error; err != nil {
			fmt.Println("skip", k, err)
		}
	}
	var n int64
	g.Model(&model.GeoKeyword{}).Count(&n)
	
	fmt.Println("keywords:", n)
}
