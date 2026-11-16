package llm

import "testing"

func TestStripArtifacts(t *testing.T) {
	cases := map[string]string{
		// The leak seen in production, at the end of a reply.
		"两边回复都到了：144 和 100\n\nFANOUT-OK\n\n<budget:token_budget>200000</budget:token_budget>": "两边回复都到了：144 和 100\n\nFANOUT-OK",
		"开头 <budget:token_budget>5</budget:token_budget> 结尾":                                  "开头  结尾",
		"lone <budget:x/> here </budget:y> end <budget:token_budget>":                         "lone  here  end",
		// Ordinary text and XML stay as they are.
		"普通回复，没有标签":                            "普通回复，没有标签",
		"<xs:element name=\"a\"/> 和 <b>粗体</b>": "<xs:element name=\"a\"/> 和 <b>粗体</b>",
		"预算 budget: 200 元":                     "预算 budget: 200 元",
	}
	for input, want := range cases {
		if got := StripArtifacts(input); got != want {
			t.Errorf("StripArtifacts(%q) = %q, want %q", input, got, want)
		}
	}
}
