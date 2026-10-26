package scheduler

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// switchTokenScheduler 构造单个个人号，AccessToken 为带
// token_source=enterprise_switch 的 JWT（enterpriseId 为空——浏览器在企业空间
// 状态下完成 OAuth 时签发的形态）。
func switchTokenScheduler(t *testing.T) (*Scheduler, *recorderUpstream) {
	t.Helper()
	r := &recorderUpstream{}
	srv := r.server()
	t.Cleanup(srv.Close)

	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u1","token_source":"enterprise_switch"}`))
	a := &auth.Auth{UID: "u1", AccessToken: "hdr." + payload + ".sig", RefreshToken: "rt", ExpiresAt: 9999999999}
	p := pool.New("")
	p.Add(a)

	up := &upstream.Client{
		HTTP:          srv.Client(),
		ChatBaseCN:    srv.URL,
		BillingBaseCN: srv.URL,
		WebBaseCN:     srv.URL,
	}
	s := New(Config{Pool: p, Upstream: up})
	return s, r
}

// TestEnterpriseSwitchTokenNotGated token_source=enterprise_switch 只记录 OAuth
// 授权来路，不代表企业身份：enterpriseId 为空的个人号行为事件照常计分
// （2026-10-26 实测：发一条 chat_request_send 后 heatmap 当天 score 33→35）。
// 因此四类行为任务都不得跳过；真企业版仍由 TestEnterpriseGateSkipsGrowthTasks 覆盖。
func TestEnterpriseSwitchTokenNotGated(t *testing.T) {
	// 夜猫子窗口钉在窗口内，避免用例判别力取决于运行时钟。
	restoreClock := nowForNightWindow
	nowForNightWindow = func() time.Time { return time.Date(2026, 10, 26, 23, 30, 0, 0, time.Local) }
	t.Cleanup(func() { nowForNightWindow = restoreClock })

	cases := []struct {
		name     string
		run      func(*Scheduler)
		endpoint string
	}{
		{"活跃上报", func(s *Scheduler) { s.RunActivityNow() }, "/v2/report"},
		{"猫猫旅行", func(s *Scheduler) { s.RunTravelNow() }, "/activity/growth/buddy/info"},
		// 夜猫子第一跳是任务列表（问还差几次）；mock 空任务表 ⇒ need=0。
		{"夜猫子", func(s *Scheduler) { s.RunBlackcatNow() }, "/v2/activity/growth/tasks"},
		{"连登管家", func(s *Scheduler) { s.RunStreakBonusNow() }, "/activity/growth/streak"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, r := switchTokenScheduler(t)
			c.run(s)
			if !r.has(c.endpoint) {
				t.Errorf("企业切换会话的个人号应照常发起 %s，实际请求=%v", c.endpoint, r.snapshot())
			}
		})
	}
}

// TestEnterpriseSwitchTokenKeepsKeepalive 切保活不受任何成长门控影响。
func TestEnterpriseSwitchTokenKeepsKeepalive(t *testing.T) {
	s, r := switchTokenScheduler(t)
	s.RunKeepaliveNow()
	if !r.has("/token/refresh") {
		t.Errorf("必须照常保活，实际请求=%v", r.snapshot())
	}
}
