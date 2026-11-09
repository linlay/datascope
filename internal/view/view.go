// Package view defines administrator-authored, self-contained HTML resources.
package view

import (
	"embed"
	"fmt"
	"regexp"
	"strings"
)

const MIMEType = "text/html"
const MaxHTMLBytes = 256 * 1024

var identifier = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

type Definition struct {
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Type        string `json:"type" yaml:"type"`
	HTML        string `json:"html" yaml:"html"`
	Published   bool   `json:"published" yaml:"published"`
}

func (v Definition) Validate() error {
	if !identifier.MatchString(v.ID) || strings.TrimSpace(v.Name) == "" || len(v.Name) > 200 || len(v.Description) > 2000 {
		return fmt.Errorf("视图需要合法标识、名称和最多 2000 字节的描述")
	}
	if v.Type != "view" {
		return fmt.Errorf("视图 type 必须为 view")
	}
	if strings.TrimSpace(v.HTML) == "" || len(v.HTML) > MaxHTMLBytes {
		return fmt.Errorf("视图 HTML 不能为空且最多 256 KiB")
	}
	return nil
}

func URI(database, id string) string { return "ui://datascope/" + database + "/views/" + id }

//go:embed templates/*
var templates embed.FS

// Templates returns reusable views without domain-specific schemas or actions.
func Templates() []Definition {
	return loadTemplates([]Definition{
		{ID: "query", Name: "通用查询视图", Description: "使用绑定参数执行只读 SQL，展示结果表格并重新查询。"},
		{ID: "action", Name: "动作预览与结果", Description: "根据动作参数生成表单，展示真实预览、本人确认链接和请求回执。"},
	})
}

// DemoViews includes the securities example for explicit demo installation only.
func DemoViews() []Definition {
	return append(Templates(), SecuritiesViews()...)
}

// SecuritiesViews returns domain-specific examples, never generic templates.
func SecuritiesViews() []Definition {
	return loadTemplates([]Definition{
		{ID: "securities_account", Name: "证券账户详情", Description: "按资金账号查看客户、资金、持仓、委托和流水，发起客户资料、账户及委托动作。"},
		{ID: "securities_customers", Name: "客户列表", Description: "按姓名或编号搜索客户，筛选客户类型与风险等级，分页查看并打开客户详情。"},
		{ID: "securities_rankings", Name: "客户多项排名", Description: "按当前可见客户范围，查看总资产、资金余额、持仓市值、成交额、成交笔数、净入金与手续费排名。"},
		{ID: "securities_customer", Name: "客户详情", Description: "按客户编号查看客户资料、多资金账户、指定日期资产汇总、持仓、成交与资金流水。"},
	})
}

func loadTemplates(items []Definition) []Definition {
	for i := range items {
		body, err := templates.ReadFile("templates/" + items[i].ID + ".html")
		if err != nil {
			panic(err)
		}
		css, _ := templates.ReadFile("templates/shared.css")
		runtime, _ := templates.ReadFile("templates/runtime.js")
		items[i].HTML = strings.Replace(string(body), "<!--shared-->", "<style>"+string(css)+"</style><script>"+string(runtime)+"</script>", 1)
		if strings.Contains(items[i].HTML, "<!--securities-->") {
			queries, _ := templates.ReadFile("templates/securities_queries.json")
			securitiesJS, _ := templates.ReadFile("templates/securities.js")
			securitiesCSS, _ := templates.ReadFile("templates/securities.css")
			items[i].HTML = strings.Replace(items[i].HTML, "<!--securities-->", "<style>"+string(securitiesCSS)+"</style><script id=\"securities-queries\" type=\"application/json\">"+string(queries)+"</script><script>"+string(securitiesJS)+"</script>", 1)
		}
		items[i].Type, items[i].Published = "view", true
	}
	return items
}
