package main

import (
	"flag"
	"log"
	"datascope-server/internal/gendata"
)

func main() {
	path := flag.String("db", "data/securities_demo.sqlite3", "输出路径，已存在则拒绝覆盖")
	schema := flag.String("schema", "sql/schema.sqlite.sql", "DDL")
	n := flag.Int("customers", 1500, "客户数")
	seed := flag.Int64("seed", 20261021, "固定随机种子")
	dictionary := flag.String("dictionary", "docs/data-dictionary.md", "数据字典输出路径，空字符串跳过")
	flag.Parse()
	if err := gendata.Generate(gendata.Options{Path: *path, Schema: *schema, Customers: *n, Seed: *seed}); err != nil {
		log.Fatal(err)
	}
	if *dictionary != "" {
		if err := gendata.WriteDictionary(*path, *dictionary); err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("生成完成: %s", *path)
}
