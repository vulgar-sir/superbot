package search

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// EngineBing marks results from the keyless fallback, so the tool can say
// they did not come from a configured search API.
const EngineBing = "bing"

// bingEndpoint is Bing's China site: reachable from mainland servers, where
// most search APIs and DuckDuckGo are not, and usable without a key.
var bingEndpoint = "https://cn.bing.com/search"

var (
	bingBlock   = regexp.MustCompile(`(?s)<li class="b_algo".*?</li>`)
	bingTitle   = regexp.MustCompile(`(?s)<h2[^>]*>\s*<a[^>]*?href="([^"]+)"[^>]*>(.*?)</a>`)
	bingCaption = regexp.MustCompile(`(?s)<div class="b_caption[^"]*"[^>]*>.*?<p[^>]*>(.*?)</p>`)
	bingPara    = regexp.MustCompile(`(?s)<p[^>]*>(.*?)</p>`)
	htmlTag     = regexp.MustCompile(`(?s)<[^>]+>`)
	spaces      = regexp.MustCompile(`\s+`)
)

// searchBing is used when the account has no search provider configured. It
// reads one Bing results page; the results are plainer than a search API's
// but keep the bot from searching blind.
func (s *Service) searchBing(ctx context.Context, query string, count int) ([]Result, error) {
	params := url.Values{}
	params.Set("q", query)
	if hasHan(query) {
		params.Set("ensearch", "0")
	} else {
		// Bing's international index answers English queries better.
		params.Set("ensearch", "1")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bingEndpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bing request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bing returned HTTP %d", resp.StatusCode)
	}
	results := parseBing(string(raw), count)
	return results, nil
}

func parseBing(page string, count int) []Result {
	var out []Result
	seen := make(map[string]bool)
	for _, block := range bingBlock.FindAllString(page, -1) {
		title := bingTitle.FindStringSubmatch(block)
		if title == nil {
			continue
		}
		link := html.UnescapeString(title[1])
		if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
			continue
		}
		if seen[link] {
			continue
		}
		seen[link] = true
		snippet := ""
		if match := bingCaption.FindStringSubmatch(block); match != nil {
			snippet = match[1]
		} else if match := bingPara.FindStringSubmatch(block); match != nil {
			snippet = match[1]
		}
		site := ""
		if parsed, err := url.Parse(link); err == nil {
			site = parsed.Hostname()
		}
		out = append(out, Result{
			Title:    plainText(title[2]),
			URL:      link,
			Snippet:  truncate(plainText(snippet), 400),
			SiteName: site,
			Engine:   EngineBing,
		})
		if len(out) >= count {
			break
		}
	}
	return out
}

func plainText(fragment string) string {
	text := html.UnescapeString(htmlTag.ReplaceAllString(fragment, ""))
	text = strings.ReplaceAll(text, " ", " ")
	return strings.TrimSpace(spaces.ReplaceAllString(text, " "))
}

func hasHan(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
