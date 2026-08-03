package service

import "testing"

func TestParseProxyList(t *testing.T) {
	raw := "socks5://a:1@h1:1080\nsocks5://a:2@h2:1080,http://h3:8080\n\nsocks5://a:1@h1:1080"
	got := parseProxyList(raw)
	want := []string{"socks5://a:1@h1:1080", "socks5://a:2@h2:1080", "http://h3:8080"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSelectProxySingle(t *testing.T) {
	raw := "socks5://a:1@h1:1080"
	for i := 0; i < 5; i++ {
		p, idx, err := ChannelProxyRotator.SelectProxy(1, raw)
		if err != nil {
			t.Fatal(err)
		}
		if p != raw || idx != 1 {
			t.Fatalf("got %q idx %d, want %q idx 1", p, idx, raw)
		}
	}
}

func TestSelectProxyRotateAndFailover(t *testing.T) {
	raw := "socks5://a:1@h1:1080\nsocks5://a:2@h2:1080"
	// 使用独立渠道 ID，避免与其他测试共享状态。
	const cid = 9001
	first, idx1, err := ChannelProxyRotator.SelectProxy(cid, raw)
	if err != nil {
		t.Fatal(err)
	}
	if idx1 != 1 {
		t.Fatalf("first idx = %d, want 1", idx1)
	}
	ChannelProxyRotator.MarkProxyFailed(cid, idx1)
	// 失败后下一次应切换到第二个代理。
	second, idx2, err := ChannelProxyRotator.SelectProxy(cid, raw)
	if err != nil {
		t.Fatal(err)
	}
	if idx2 != 2 {
		t.Fatalf("after failover idx = %d, want 2", idx2)
	}
	if second == first {
		t.Fatalf("failover did not switch proxy: %q", second)
	}
}

func TestSelectProxyRotationThreshold(t *testing.T) {
	raw := "socks5://a:1@h1:1080\nsocks5://a:2@h2:1080"
	const cid = 9002
	origRequests := ChannelProxyRotateRequests
	origSeconds := ChannelProxyRotateSeconds
	ChannelProxyRotateRequests = 2
	ChannelProxyRotateSeconds = 0
	defer func() {
		ChannelProxyRotateRequests = origRequests
		ChannelProxyRotateSeconds = origSeconds
	}()
	_, idx1, _ := ChannelProxyRotator.SelectProxy(cid, raw)
	_, idx2, _ := ChannelProxyRotator.SelectProxy(cid, raw)
	// 前两次使用代理 1，第三次请求触发轮换到 2。
	if idx1 != 1 || idx2 != 1 {
		t.Fatalf("early rotation unexpected: idx1=%d idx2=%d", idx1, idx2)
	}
	_, idx3, _ := ChannelProxyRotator.SelectProxy(cid, raw)
	if idx3 != 2 {
		t.Fatalf("third idx = %d, want 2", idx3)
	}
	// 再过两次请求后轮换回 1。
	_, idx4, _ := ChannelProxyRotator.SelectProxy(cid, raw)
	_, idx5, _ := ChannelProxyRotator.SelectProxy(cid, raw)
	if idx4 != 2 || idx5 != 1 {
		t.Fatalf("rotation back unexpected: idx4=%d idx5=%d", idx4, idx5)
	}
}

func TestSelectProxyAllFailedRecovers(t *testing.T) {
	raw := "socks5://a:1@h1:1080\nsocks5://a:2@h2:1080"
	const cid = 9003
	_, i1, _ := ChannelProxyRotator.SelectProxy(cid, raw)
	_, i2, _ := ChannelProxyRotator.SelectProxy(cid, raw)
	ChannelProxyRotator.MarkProxyFailed(cid, i1)
	ChannelProxyRotator.MarkProxyFailed(cid, i2)
	// 两个都在冷却：应清空冷却并仍能返回一个代理。
	_, idx, err := ChannelProxyRotator.SelectProxy(cid, raw)
	if err != nil {
		t.Fatal(err)
	}
	if idx < 1 || idx > 2 {
		t.Fatalf("idx = %d out of range", idx)
	}
}

func TestSelectProxyConfigChangedResets(t *testing.T) {
	const cid = 9004
	raw1 := "socks5://a:1@h1:1080\nsocks5://a:2@h2:1080"
	raw2 := "socks5://b:1@h3:1080\nsocks5://b:2@h4:1080"
	_, i1, _ := ChannelProxyRotator.SelectProxy(cid, raw1)
	ChannelProxyRotator.MarkProxyFailed(cid, i1)
	// 配置变化后轮换状态应重置，第一个代理不再冷却。
	p, idx, err := ChannelProxyRotator.SelectProxy(cid, raw2)
	if err != nil {
		t.Fatal(err)
	}
	if idx != 1 {
		t.Fatalf("idx = %d, want 1", idx)
	}
	if p != "socks5://b:1@h3:1080" {
		t.Fatalf("unexpected proxy %q", p)
	}
}
