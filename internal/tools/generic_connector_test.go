package tools

import (
	"strings"
	"testing"
)

func TestCheckReadOnlySQLAccepts(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"plain select", "SELECT 1", "SELECT 1"},
		{"lowercase with whitespace", "  select * from users  ", "select * from users"},
		{"trailing semicolon", "SELECT 1;", "SELECT 1"},
		{"trailing semicolon and whitespace", "SELECT 1 ;  \n", "SELECT 1"},
		{"trailing semicolon then comments", "SELECT 1; -- done\n/* bye */", "SELECT 1"},
		{"keyword inside string", "SELECT 'delete me'", "SELECT 'delete me'"},
		{"semicolon inside string", "SELECT 'a;b' AS v", "SELECT 'a;b' AS v"},
		{"escaped quote in string", "SELECT 'it''s; drop table x'", "SELECT 'it''s; drop table x'"},
		{"E string with backslash quote", `SELECT E'it\'s; delete from t'`, `SELECT E'it\'s; delete from t'`},
		{"plain string ending in backslash", `SELECT 'C:\' AS p`, `SELECT 'C:\' AS p`},
		{"quoted identifier", `SELECT "update", "delete" FROM t`, `SELECT "update", "delete" FROM t`},
		{"quoted identifier with escaped quote", `SELECT "a""; drop" FROM t`, `SELECT "a""; drop" FROM t`},
		{"line comment with keyword", "SELECT 1 -- delete everything; drop\n", "SELECT 1 -- delete everything; drop"},
		{"leading comments", "-- insert\n/* update */ SELECT 1", "-- insert\n/* update */ SELECT 1"},
		{"nested block comment", "SELECT /* a /* drop; */ delete */ 1", "SELECT /* a /* drop; */ delete */ 1"},
		{"dollar quoted string", "SELECT $$delete; drop$$", "SELECT $$delete; drop$$"},
		{"tagged dollar quoted string", "SELECT $q$ it's; update $q$ AS v", "SELECT $q$ it's; update $q$ AS v"},
		{"positional parameter", "SELECT * FROM t WHERE id = $1", "SELECT * FROM t WHERE id = $1"},
		{"legitimate cte", "WITH recent AS (SELECT id FROM orders WHERE created_at > now() - interval '1 day') SELECT count(*) FROM recent", ""},
		{"recursive cte", "WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 5) SELECT * FROM n", ""},
		{"show", "SHOW server_version", "SHOW server_version"},
		{"explain", "EXPLAIN SELECT * FROM t", "EXPLAIN SELECT * FROM t"},
		{"explain options without analyze", "EXPLAIN (FORMAT JSON, COSTS off) SELECT 1", ""},
		{"keyword as part of identifier", "SELECT updated_at, deleted, created_by, offset_do, settings FROM t", ""},
		{"analyze column outside explain", "SELECT analyze_count FROM stats", ""},
		{"unicode", "SELECT '删除' AS 名称", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkReadOnlySQL(tc.sql)
			if err != nil {
				t.Fatalf("unexpected rejection: %v", err)
			}
			want := tc.want
			if want == "" {
				want = tc.sql
			}
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestCheckReadOnlySQLRejects(t *testing.T) {
	const (
		notReadOnly  = "只允许只读查询"
		multiple     = "一次只能执行一条查询"
		analyze      = "EXPLAIN ANALYZE"
		unterminated = "未闭合"
	)
	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"data-modifying cte", "WITH x AS (DELETE FROM users RETURNING *) SELECT * FROM x", notReadOnly},
		{"data-modifying cte lowercase", "with x as (update users set a = 1 returning *) select * from x", notReadOnly},
		{"insert cte", "WITH x AS (INSERT INTO t VALUES (1) RETURNING id) SELECT * FROM x", notReadOnly},
		{"explain analyze delete", "EXPLAIN ANALYZE DELETE FROM users", notReadOnly},
		{"explain analyze select", "EXPLAIN ANALYZE SELECT * FROM users", analyze},
		{"explain option analyze", "explain (analyze, buffers) select 1", analyze},
		{"explain analyse spelling", "EXPLAIN ANALYSE SELECT 1", analyze},
		{"explain analyze split by comment", "EXPLAIN /* x */ ANALYZE SELECT 1", analyze},
		{"multiple statements", "SELECT 1; DELETE FROM users", multiple},
		{"multiple selects", "SELECT 1; SELECT 2", multiple},
		{"multiple selects both terminated", "SELECT 1; SELECT 2;", multiple},
		{"double semicolon", "SELECT 1;;", multiple},
		{"statement after comment", "SELECT 1; -- note\nSELECT 2", multiple},
		{"string after semicolon", "SELECT 1; 'x'", multiple},
		{"comment hides nothing", "SELECT 1 /* */; DROP TABLE t", multiple},
		{"keyword glued to comment", "SELECT 1 FROM t WHERE true/**/;/**/DELETE/**/FROM t", multiple},
		{"keyword split from select by comment", "WITH x AS (/**/DELETE/**/FROM t RETURNING 1) SELECT 1", notReadOnly},
		{"string closed before keyword", "SELECT 'a' FROM t FOR UPDATE", notReadOnly},
		{"backslash does not hide keyword in plain string", `WITH x AS (SELECT 'C:\') , y AS (DELETE FROM t RETURNING 1) SELECT ''`, notReadOnly},
		{"dollar quote closed before keyword", "WITH x AS (SELECT $$a$$), y AS (DELETE FROM t RETURNING 1) SELECT 1", notReadOnly},
		{"insert", "INSERT INTO t VALUES (1)", notReadOnly},
		{"update", "UPDATE t SET a = 1", notReadOnly},
		{"delete", "DELETE FROM t", notReadOnly},
		{"merge", "WITH s AS (SELECT 1) MERGE INTO t USING s ON true WHEN MATCHED THEN DO NOTHING", notReadOnly},
		{"truncate", "TRUNCATE t", notReadOnly},
		{"drop", "DROP TABLE t", notReadOnly},
		{"alter", "ALTER TABLE t ADD c int", notReadOnly},
		{"create", "CREATE TABLE t (a int)", notReadOnly},
		{"grant", "GRANT ALL ON t TO public", notReadOnly},
		{"revoke", "REVOKE ALL ON t FROM public", notReadOnly},
		{"copy", "COPY t TO PROGRAM 'id'", notReadOnly},
		{"call", "CALL p()", notReadOnly},
		{"do block", "DO $$ BEGIN DELETE FROM t; END $$", notReadOnly},
		{"vacuum", "VACUUM t", notReadOnly},
		{"reindex", "REINDEX TABLE t", notReadOnly},
		{"refresh", "REFRESH MATERIALIZED VIEW v", notReadOnly},
		{"lock", "LOCK TABLE t", notReadOnly},
		{"set", "SET transaction_read_only = off", notReadOnly},
		{"reset", "RESET ALL", notReadOnly},
		{"begin", "BEGIN", notReadOnly},
		{"commit", "COMMIT", notReadOnly},
		{"parenthesised", "(SELECT 1)", notReadOnly},
		{"empty", "", notReadOnly},
		{"only whitespace and comments", "  -- nothing\n/* here */ ", notReadOnly},
		{"only semicolon", ";", notReadOnly},
		{"prefix is not a word match", "selection FROM t", notReadOnly},
		{"unterminated string", "SELECT 'abc", unterminated},
		{"unterminated quoted identifier", `SELECT "abc`, unterminated},
		{"unterminated block comment", "SELECT 1 /* oops", unterminated},
		{"unterminated nested comment", "SELECT 1 /* a /* b */", unterminated},
		{"unterminated dollar quote", "SELECT $x$ abc $y$", unterminated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkReadOnlySQL(tc.sql)
			if err == nil {
				t.Fatalf("accepted as %q", got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}
