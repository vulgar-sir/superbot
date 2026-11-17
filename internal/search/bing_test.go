package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A trimmed copy of a real cn.bing.com result block.
const bingPage = `<ol id="b_results">
<li class="b_algo" data-id iid=SERP.5288><div class="b_tpcn"><a class="tilk" aria-label="tencent.com" href="http://ai.tencent.com/"><div class="tptxt"><div class="tptt">tencent.com</div><cite>http://ai.tencent.com</cite></div></a></div><h2><a target="_blank" target="_blank" href="http://ai.tencent.com/" h="ID=SERP,5141.2">Yuanbao–Tencent's All-in-One <strong>AI</strong> Assistant</a></h2><div class="b_caption"><p class="b_lineclamp2" data-rslinkclamp-iid="">4 天之前&ensp;&#0183;&ensp;元宝是腾讯推出的全能 AI 助手 &amp; 写作</p></div></li>
<li class="b_algo"><h2><a href="https://www.cnnic.net.cn/n4/2025/0117/c88-11229.html">第55次《中国互联网络发展状况统计报告》</a></h2><p>截至2024年12月，我国网民规模达11.08亿人</p></li>
<li class="b_algo"><h2><a href="javascript:void(0)">not a link</a></h2></li>
<li class="b_algo"><h2><a href="http://ai.tencent.com/">duplicate</a></h2></li>
</ol>`

func TestParseBing(t *testing.T) {
	results := parseBing(bingPage, 10)
	if len(results) != 2 {
		t.Fatalf("got %d results: %+v", len(results), results)
	}
	first := results[0]
	if first.URL != "http://ai.tencent.com/" || first.Title != "Yuanbao–Tencent's All-in-One AI Assistant" {
		t.Fatalf("first result = %+v", first)
	}
	if !strings.Contains(first.Snippet, "元宝是腾讯推出的全能 AI 助手 & 写作") || first.SiteName != "ai.tencent.com" || first.Engine != EngineBing {
		t.Fatalf("first result = %+v", first)
	}
	if results[1].Snippet != "截至2024年12月，我国网民规模达11.08亿人" {
		t.Fatalf("second snippet = %q", results[1].Snippet)
	}
	if got := parseBing(bingPage, 1); len(got) != 1 {
		t.Fatalf("count not respected: %d", len(got))
	}
}

func TestSearchBingPicksIndexByScript(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		_, _ = w.Write([]byte(bingPage))
	}))
	defer server.Close()
	old := bingEndpoint
	bingEndpoint = server.URL
	defer func() { bingEndpoint = old }()

	s := &Service{client: server.Client()}
	if _, err := s.searchBing(context.Background(), "AI客服 复购意愿", 5); err != nil {
		t.Fatal(err)
	}
	if _, err := s.searchBing(context.Background(), "chatbot service quality", 5); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(queries[0], "ensearch=0") || !strings.Contains(queries[1], "ensearch=1") {
		t.Fatalf("queries = %v", queries)
	}
}
