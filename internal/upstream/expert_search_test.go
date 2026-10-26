package upstream

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
)

// TestMarketExpertSearchRequest 专家市场搜索与推荐榜共用同一端点：搜索必须带
// keyword + expert_type=agent（按任务点名专家反查），推荐榜保持 sort_by=reco_rank。
func TestMarketExpertSearchRequest(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/portal/operation-platform/market/expert/list" {
			t.Errorf("path=%s want /portal/operation-platform/market/expert/list", r.URL.Path)
		}
		if r.Header.Get("X-User-Id") != "u-ex" {
			t.Errorf("X-User-Id=%q want u-ex", r.Header.Get("X-User-Id"))
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("body unmarshal: %v", err)
		}
		bodies = append(bodies, body)
		w.Write([]byte(`{"code":0,"data":{"experts":[{"expert_id":"ex_FbY5nlWTqDod","display_name_zh":"斗音音","profession_zh":"抖音策略师","version":"1.0.2","expert_type":"agent"}]}}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), ChatBaseCN: srv.URL}
	a := &auth.Auth{AccessToken: "at", UID: "u-ex"}

	got, err := c.MarketExpertSearch(a, "抖音策略师")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 || got[0].ExpertID != "ex_FbY5nlWTqDod" || got[0].DisplayNameZH != "斗音音" {
		t.Fatalf("搜索结果解析错: %+v", got)
	}
	if _, err := c.MarketExpertList(a, "agent"); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("requests=%d want 2", len(bodies))
	}
	search := bodies[0]
	if search["keyword"] != "抖音策略师" || search["expert_type"] != "agent" {
		t.Errorf("搜索请求参数错: %v", search)
	}
	if search["page_size"].(float64) != 50 {
		t.Errorf("搜索 page_size=%v want 50", search["page_size"])
	}
	list := bodies[1]
	if list["sort_by"] != "reco_rank" || list["sort_order"] != "desc" || list["expert_type"] != "agent" {
		t.Errorf("推荐榜请求参数错: %v", list)
	}
	if _, ok := list["keyword"]; ok {
		t.Errorf("推荐榜不应带 keyword: %v", list)
	}
}
