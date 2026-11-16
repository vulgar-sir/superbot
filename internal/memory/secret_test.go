package memory

import (
	"strings"
	"testing"
)

func TestLooksLikeSecret(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"openai key", "我的 key 是 sk-proj-AbCdEf0123456789AbCdEf0123456789", true},
		{"anthropic key", "sk-ant-api03-Zx9Yw8Vu7Ts6Rq5Po4Nm3Lk2Ji1Hg0", true},
		{"github token", "token ghp_16C7e42F292c6912E7710c838347Ae178B4a", true},
		{"github fine-grained", "github_pat_11ABCDEFG0abcdefghijkl_mnopqrstuvwxyz0123456789", true},
		{"slack bot token", "xoxb-1234567890-0987654321-AbCdEfGhIjKlMnOpQrStUvWx", true},
		{"slack user token", "use xoxp-123456789012-abcdefghij", true},
		{"aws access key id", "AWS key AKIAIOSFODNN7EXAMPLE for the bucket", true},
		{"jwt", "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c", true},
		{"pem header", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow...", true},
		{"openssh pem header", "-----BEGIN OPENSSH PRIVATE KEY-----", true},
		{"password assignment", "db password=hunter2!", true},
		{"password colon", "Password: Tr0ub4dor&3", true},
		{"api key assignment", "API_KEY = abc123def", true},
		{"chinese password", "路由器密码：wifi-8888", true},
		{"chinese password is", "服务器密码是 Abc12345", true},
		{"hex run", "签名密钥 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", true},
		{"base64 run", "secret blob: Zm9vYmFyQmF6MTIzNDU2Nzg5MEFCQ0RFRkdISUpLTE1OT1A=", true},
		{"random url-safe token", "token 7hG2kP9xLq4ZtR8vBn3WmY6cJd1FsA5eUo0iKp2Xw", true},

		{"plain chinese", "用户喜欢喝无糖的冰美式咖啡", false},
		{"plain english", "The user prefers dark mode and a compact layout.", false},
		{"mentions password without value", "用户把密码都放在 1Password 里，不要问他要密码", false},
		{"password policy", "The password policy requires rotation every 90 days", false},
		{"empty password field", "password:", false},
		{"windows path", `项目在 E:\project\superbot\internal\memory\service.go`, false},
		{"long unix path", "配置文件在 /home/deploy/applications/superbot/configuration/production/settings", false},
		{"camel case path", "see internal/httpapi/MemoriesHandlerFactory/CreateUserMemoryRequestValidator.go", false},
		{"url", "文档地址 https://example.com/docs/getting-started/installation-guide-for-linux-servers", false},
		{"hex colour", "品牌主色是 #1a2b3c，辅色 #FFEE00", false},
		{"short hex", "commit abc1234 fixed the bug", false},
		{"uuid", "bot id 是 550e8400-e29b-41d4-a716-446655440000", false},
		{"upper uuid", "ID 550E8400-E29B-41D4-A716-446655440000", false},
		{"two uuids", "a3bb189e-8bf9-3888-9912-ace4e6543002 和 123e4567-e89b-12d3-a456-426614174000", false},
		{"phone and port", "部署端口是 32608，电话 13800138000", false},
		{"long digits", "订单号 20261116000012345678901234567890123456", false},
		{"long word", strings.Repeat("a", 40), false},
		{"snake case identifier", "环境变量 SUPERBOT_MEMORY_REVIEW_IDLE_SECONDS_FOR_TESTING 控制空闲时间", false},
		{"max tokens", "max_tokens: 4096", false},
		{"blank", "   ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LooksLikeSecret(tc.text); got != tc.want {
				t.Fatalf("LooksLikeSecret(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestValidateContentRefusesSecrets(t *testing.T) {
	if _, err := validateContent("sk-proj-AbCdEf0123456789AbCdEf0123456789"); err != ErrSecretContent {
		t.Fatalf("err = %v, want ErrSecretContent", err)
	}
	if _, err := validateContent("   "); err == nil {
		t.Fatal("blank content must be refused")
	}
	if _, err := validateContent(strings.Repeat("好", maxContentRunes+1)); err == nil {
		t.Fatal("over-long content must be refused")
	}
	if got, err := validateContent("  用户住在上海  "); err != nil || got != "用户住在上海" {
		t.Fatalf("validateContent = %q, %v", got, err)
	}
}
