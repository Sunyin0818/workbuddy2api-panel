package panel

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// TestExpertExperienceSpecs 五条「点名专家」任务的固定参数必须完整、id 与任务
// jump_url 一致（2026-10-23 用 CN 账号核对），且都进了 autoActions 表。
//
// 缺一条 = 面板不渲染「一键完成」；名称/职业名互换 = 服务端判据不计数
// （name 取昵称、expertTitle 取职业名）。
func TestExpertExperienceSpecs(t *testing.T) {
	want := map[string]string{
		"WorkBuddy_Valorant":            "ex_xQHHD1u5Jkua",
		"expert_use_maidehao":           "ex_GDl31WahyIH0",
		"expert_use_douyinyin":          "ex_FbY5nlWTqDod",
		"expert_use_shangjia_xuanchuan": "ex_Weo3x2EZuSDm",
		"expert_use_bowangwang":         "ex_WZY136wV5aMt",
	}
	if len(expertExperienceTasks) != len(want) {
		t.Fatalf("任务数 = %d, want %d", len(expertExperienceTasks), len(want))
	}
	for code, id := range want {
		spec, ok := expertExperienceTasks[code]
		if !ok {
			t.Fatalf("%s 未登记", code)
		}
		if spec.ExpertID != id {
			t.Errorf("%s 的 expert_id = %q, want %q", code, spec.ExpertID, id)
		}
		if spec.DisplayNameZH == "" || spec.ProfessionZH == "" || spec.Version == "" {
			t.Errorf("%s 参数不全: display=%q profession=%q version=%q",
				code, spec.DisplayNameZH, spec.ProfessionZH, spec.Version)
		}
		if spec.ExpertType != "agent" {
			t.Errorf("%s 的 expert_type = %q, want agent", code, spec.ExpertType)
		}
		if act := autoActionFor(code); act == nil || act.TaskCode != code || act.run == nil {
			t.Errorf("%s 未登记为可自动完成动作", code)
		}
	}
	if !isNamedExpertTask("expert_use_maidehao") || !isNamedExpertTask("WorkBuddy_Valorant") {
		t.Fatal("点名专家族判定漏掉了已登记任务")
	}
}

// TestExpertIDFromJump 任务 deeplink 的两种形态都要能解出 expertId；无该参数的
// 任务码（WorkBuddy_Valorant 的 workbuddy://expert）返回空串，由固定参数兜底。
func TestExpertIDFromJump(t *testing.T) {
	cases := []struct {
		name string
		jump string
		want string
	}{
		{
			name: "urlencoded deeplink",
			jump: "https://www.workbuddy.cn/work/launch/?deeplink=workbuddy%3A%2F%2Fexperts%3FexpertId%3Dex_FbY5nlWTqDod%26buddy_type%3Dworkbuddy",
			want: "ex_FbY5nlWTqDod",
		},
		{
			name: "明文 query",
			jump: "workbuddy://experts?expertId=ex_WZY136wV5aMt&buddy_type=workbuddy",
			want: "ex_WZY136wV5aMt",
		},
		{name: "无 expertId", jump: "workbuddy://expert", want: ""},
		{name: "空串", jump: "", want: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := expertIDFromJump(c.jump); got != c.want {
				t.Fatalf("expertIDFromJump(%q) = %q, want %q", c.jump, got, c.want)
			}
		})
	}
}

// TestQuotedTitleName 任务标题书名号取专家名；无书名号返回空（回落固定参数）。
func TestQuotedTitleName(t *testing.T) {
	cases := map[string]string{
		"体验「抖音策略师」":       "抖音策略师",
		"体验「商家宣传页与海报设计师」": "商家宣传页与海报设计师",
		"体验「无畏契约拼豆专家」":    "无畏契约拼豆专家",
		"没有书名号的标题":        "",
		"未闭合「专家":          "",
	}
	for title, want := range cases {
		if got := quotedTitleName(title); got != want {
			t.Fatalf("quotedTitleName(%q) = %q, want %q", title, got, want)
		}
	}
}

// TestNamedExpertDynamicActions 前缀通配：活动新增 expert_use_* 时无需改代码即可
// 出现动作；非本族任务码不受影响。
func TestNamedExpertDynamicActions(t *testing.T) {
	act := autoActionFor("expert_use_future_task")
	if act == nil || act.TaskCode != "expert_use_future_task" || act.run == nil {
		t.Fatalf("未登记的 expert_use_* 应动态生成动作, got=%+v", act)
	}
	if autoActionFor("no_such_task") != nil {
		t.Fatal("未知任务不应有动作")
	}
	if isNamedExpertTask("expert_5") {
		t.Fatal("expert_5 是静态专家任务，不应命中点名专家族")
	}
}

// TestMergeNamedExpertActions 一键完成全部的动态展开：只补本账号实际下发、静态表
// 未登记的「点名专家」码，去重且不改变静态项顺序。
func TestMergeNamedExpertActions(t *testing.T) {
	base := []autoAction{
		{TaskCode: "chat_5"},
		{TaskCode: "expert_use_maidehao"},
		{TaskCode: "first_buddy"},
		{TaskCode: "WorkBuddy_Valorant"},
	}
	tasks := []upstream.Task{
		{TaskCode: "expert_use_maidehao"},     // 已登记，去重
		{TaskCode: "chat_5"},                  // 非本族
		{TaskCode: " expert_use_new_expert "}, // 空白裁剪后动态追加
		{TaskCode: "expert_use_new_expert"},   // 重复下发，去重
		{TaskCode: ""},                        // 空码跳过
		{TaskCode: "WorkBuddy_Valorant"},      // 已登记，去重
	}
	got := mergeNamedExpertActions(base, tasks)
	wantCodes := []string{"chat_5", "expert_use_maidehao", "first_buddy", "WorkBuddy_Valorant", "expert_use_new_expert"}
	if len(got) != len(wantCodes) {
		t.Fatalf("动作数 = %d, want %d: %+v", len(got), len(wantCodes), codesOf(got))
	}
	for i, code := range wantCodes {
		if got[i].TaskCode != code {
			t.Fatalf("第 %d 项 = %q, want %q（顺序 %v）", i, got[i].TaskCode, code, codesOf(got))
		}
	}
	if got[4].run == nil {
		t.Fatal("动态项缺少 run")
	}
}

func codesOf(acts []autoAction) []string {
	out := make([]string, 0, len(acts))
	for _, a := range acts {
		out = append(out, a.TaskCode)
	}
	return out
}

// TestPickExpertByID 同名专家很多，市场结果必须按 expert_id 精确落位，不能取首条。
func TestPickExpertByID(t *testing.T) {
	list := []upstream.MarketExpert{
		{ExpertID: "ex_other", DisplayNameZH: "斗音音"},
		{ExpertID: "ex_FbY5nlWTqDod", DisplayNameZH: "斗音音"},
	}
	got, ok := pickExpertByID(list, "ex_FbY5nlWTqDod")
	if !ok || got.ExpertID != "ex_FbY5nlWTqDod" {
		t.Fatalf("pickExpertByID 未按 id 精确落位: ok=%v got=%q", ok, got.ExpertID)
	}
	if _, ok := pickExpertByID(list, "ex_missing"); ok {
		t.Fatal("不存在的 id 不应命中")
	}
}

// TestNamedExpertFrontendLogic 面板按钮依赖前端白名单：后端已登记的动作必须在前端
// 出现，且随活动下发的 expert_use_* 走动态判定——否则任务中心只显示「接受」。
// 直接执行 app.js 的相关片段（而非文本 grep），语法/逻辑错误都会被抓到。
func TestNamedExpertFrontendLogic(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; skipping JS execution check")
	}
	js, err := os.ReadFile("app.js")
	if err != nil {
		t.Fatalf("读 app.js: %v", err)
	}
	start := bytes.Index(js, []byte("const AUTO_TASKS = {"))
	end := bytes.Index(js, []byte("function openTasks"))
	if start < 0 || end <= start {
		t.Fatalf("app.js 里找不到 AUTO_TASKS / 点名专家判定片段: start=%d end=%d", start, end)
	}
	codes := make([]string, 0, len(expertExperienceTasks))
	for code := range expertExperienceTasks {
		codes = append(codes, "'"+code+"'")
	}
	script := string(js[start:end]) + `
const want = [` + strings.Join(codes, ",") + `];
for (const code of want) {
  if (!AUTO_TASKS[code]) throw new Error('AUTO_TASKS 缺 ' + code);
}
if (!NAMED_EXPERT_DESC) throw new Error('缺 NAMED_EXPERT_DESC');
if (!isNamedExpertTask('expert_use_future_task')) throw new Error('expert_use_* 未动态识别');
if (!isNamedExpertTask('WorkBuddy_Valorant')) throw new Error('WorkBuddy_Valorant 未识别');
if (isNamedExpertTask('expert_5')) throw new Error('expert_5 不应命中动态族');
if (isNamedExpertTask('chat_5')) throw new Error('chat_5 不应命中动态族');
`
	if out, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("app.js 点名专家判定逻辑失败: %v\n%s", err, out)
	}
}

// TestRunNamedExpertUseReportsBoundExpert 端到端：任务 jump_url 绑定的专家 id 必须
// 成为判据目标（市场搜索里同名专家有多条时按 id 精确落位），且 actual_use 用
// Expert_lighthouse 口径（mode=LOCAL、type 空、cost=0、requestId=服务端 id）。
func TestRunNamedExpertUseReportsBoundExpert(t *testing.T) {
	const serverReqID = "cmb-0123456789abcdef0123456789abcdef"
	var (
		mu       sync.Mutex
		reports  [][]map[string]any
		keywords []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/activity/growth/tasks":
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"tasks": []map[string]any{
				{
					"task_code": "expert_use_douyinyin", "title": "体验「抖音策略师」",
					"target": 1, "current": 0, "accept_status": "accepted",
					"jump_url": "workbuddy://experts?expertId=ex_FbY5nlWTqDod&buddy_type=workbuddy",
				},
			}}})
		case "/portal/operation-platform/market/expert/list":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			keywords = append(keywords, body["keyword"].(string))
			mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"experts": []map[string]any{
				{"expert_id": "ex_other", "display_name_zh": "斗音音", "profession_zh": "抖音策略师", "version": "0.9.0", "expert_type": "agent"},
				{"expert_id": "ex_FbY5nlWTqDod", "display_name_zh": "斗音音", "profession_zh": "抖音策略师", "version": "1.0.2", "expert_type": "agent"},
			}}})
		case "/v2/report":
			var evs []map[string]any
			json.NewDecoder(r.Body).Decode(&evs)
			mu.Lock()
			reports = append(reports, evs)
			mu.Unlock()
			w.Write([]byte(`{"code":0,"msg":"OK"}`))
		case "/v2/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte(`data: {"id":"` + serverReqID + `","choices":[]}` + "\n\n"))
		default:
			t.Logf("unexpected path: %s", r.URL.Path)
			w.Write([]byte(`{"code":0,"data":{}}`))
		}
	}))
	defer srv.Close()

	p := &Panel{cfg: Config{Upstream: &upstream.Client{
		HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL, WebBaseCN: srv.URL,
	}}}
	msg, err := p.runNamedExpertUse(&auth.Auth{AccessToken: "at", UID: "u-ex"}, "expert_use_douyinyin")
	if err != nil {
		t.Fatalf("runNamedExpertUse: %v", err)
	}
	if !strings.Contains(msg, "斗音音") || !strings.Contains(msg, "ex_FbY5nlWTqDod") {
		t.Fatalf("message = %q, 应回显专家名与 id", msg)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keywords) != 1 || keywords[0] != "抖音策略师" {
		t.Fatalf("市场搜索关键词 = %v, want [抖音策略师]", keywords)
	}
	var actual map[string]any
	summoned := false
	hasExpert := false
	for _, batch := range reports {
		for _, ev := range batch {
			switch ev["eventCode"] {
			case "expert_summoned":
				if ev["id"] == "ex_FbY5nlWTqDod" {
					summoned = true
				}
			case "agent_task_created":
				if ev["has_expert"] == true && ev["expert_id"] == "ex_FbY5nlWTqDod" {
					hasExpert = true
				}
			case "expert_actual_use":
				actual = ev
			}
		}
	}
	if !summoned {
		t.Fatal("召唤链未使用任务绑定的专家 id")
	}
	if !hasExpert {
		t.Fatal("agent_task_created 缺 has_expert/expert_id")
	}
	if actual == nil {
		t.Fatal("未上报 expert_actual_use")
	}
	if actual["id"] != "ex_FbY5nlWTqDod" || actual["mode"] != "LOCAL" ||
		actual["type"] != "" || actual["cost"] != float64(0) {
		t.Errorf("actual_use 口径错: id=%v mode=%v type=%v cost=%v",
			actual["id"], actual["mode"], actual["type"], actual["cost"])
	}
	if actual["requestId"] != serverReqID {
		t.Errorf("requestId = %v, want 服务端 id %q", actual["requestId"], serverReqID)
	}
	if actual["version"] != "1.0.2" {
		t.Errorf("version = %v, want 市场校正后的 1.0.2", actual["version"])
	}
}
