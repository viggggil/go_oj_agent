package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
)

type testcase struct{ input, output string }
type problem struct {
	title, slug, description string
	difficulty               int
	tags                     []string
	tests                    []testcase
}
type client struct {
	baseURL, token string
	http           *http.Client
}

func main() {
	baseURL := env("PROBLEM_SEED_BASE_URL", "http://127.0.0.1:8080")
	account, password := os.Getenv("PROBLEM_SEED_ADMIN_ACCOUNT"), os.Getenv("PROBLEM_SEED_ADMIN_PASSWORD")
	if account == "" || password == "" {
		fatal("PROBLEM_SEED_ADMIN_ACCOUNT and PROBLEM_SEED_ADMIN_PASSWORD are required")
	}
	c := &client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 45 * time.Second}}
	if err := c.login(account, password); err != nil {
		fatal("login: %v", err)
	}
	existing, err := c.existingSlugs()
	if err != nil {
		fatal("list problems: %v", err)
	}
	created := 0
	for _, p := range catalog() {
		if existing[p.slug] {
			fmt.Printf("skip existing %s\n", p.slug)
			continue
		}
		id, err := c.createProblem(p)
		if err != nil {
			fatal("create %s: %v", p.slug, err)
		}
		for index, test := range p.tests {
			if err = c.upload(id, index+1, test); err != nil {
				fatal("upload %s case %d: %v", p.slug, index+1, err)
			}
		}
		created++
		fmt.Printf("created %d %s with %d testcases\n", id, p.slug, len(p.tests))
	}
	fmt.Printf("seed complete: %d created, %d already present\n", created, len(catalog())-created)
}

func (c *client) login(account, password string) error {
	body, _ := json.Marshal(map[string]string{"account": account, "password": password})
	request, _ := http.NewRequest(http.MethodPost, c.baseURL+"/api/v1/auth/login", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return responseError(response)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err = json.NewDecoder(response.Body).Decode(&token); err != nil {
		return err
	}
	c.token = token.AccessToken
	return nil
}

func (c *client) existingSlugs() (map[string]bool, error) {
	result := map[string]bool{}
	for page := 1; ; page++ {
		var payload struct {
			Items []struct {
				Slug string `json:"slug"`
			} `json:"items"`
			Page struct {
				Total int `json:"total"`
			} `json:"page"`
		}
		request, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/problems?page.page=%d&page.page_size=100", c.baseURL, page), nil)
		c.authorize(request)
		response, err := c.http.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK {
			defer response.Body.Close()
			return nil, responseError(response)
		}
		err = json.NewDecoder(response.Body).Decode(&payload)
		response.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, item := range payload.Items {
			result[item.Slug] = true
		}
		if len(result) >= payload.Page.Total || len(payload.Items) == 0 {
			return result, nil
		}
	}
}

func (c *client) createProblem(p problem) (int64, error) {
	payload := map[string]any{"problem": map[string]any{"title": p.title, "slug": p.slug, "description": p.description, "difficulty": p.difficulty, "time_limit_ms": 1000, "memory_limit_kb": 262144, "tags": p.tags}}
	body, _ := json.Marshal(payload)
	request, _ := http.NewRequest(http.MethodPost, c.baseURL+"/api/v1/problems", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	c.authorize(request)
	response, err := c.http.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, responseError(response)
	}
	var created struct {
		Problem struct {
			ID int64 `json:"id"`
		} `json:"problem"`
	}
	if err = json.NewDecoder(response.Body).Decode(&created); err != nil {
		return 0, err
	}
	return created.Problem.ID, nil
}

func (c *client) upload(problemID int64, caseNo int, test testcase) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("case_no", fmt.Sprint(caseNo))
	input, _ := writer.CreateFormFile("input", fmt.Sprintf("%d.in", caseNo))
	_, _ = io.WriteString(input, test.input)
	output, _ := writer.CreateFormFile("output", fmt.Sprintf("%d.out", caseNo))
	_, _ = io.WriteString(output, test.output)
	if err := writer.Close(); err != nil {
		return err
	}
	request, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/problems/%d/testcases/upload", c.baseURL, problemID), &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	c.authorize(request)
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return responseError(response)
	}
	return nil
}
func (c *client) authorize(request *http.Request) {
	request.Header.Set("Authorization", "Bearer "+c.token)
}
func responseError(response *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	return fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
}
func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
func fatal(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...); os.Exit(1) }

func catalog() []problem {
	return []problem{
		{title: "两数目标和", slug: "pair-sum-index", difficulty: 1, tags: []string{"array", "hash-table"}, description: "给定 n 个整数和目标值 target，请找出两个不同位置，使这两个位置上的数之和等于 target。保证恰好存在一组答案。\n\n输入格式：第一行是 n 和 target，第二行是 n 个整数。\n输出格式：按从小到大输出两个位置的 1-based 编号。", tests: []testcase{{"4 9\n2 7 11 15\n", "1 2\n"}, {"5 6\n3 2 4 8 1\n", "2 3\n"}}},
		{title: "括号序列检查", slug: "balanced-bracket-sequence", difficulty: 1, tags: []string{"stack", "string"}, description: "给定一个只包含圆括号、方括号和花括号的字符串，判断每个右括号是否与最近尚未匹配的同类左括号配对。\n\n输入格式：一行非空字符串。\n输出格式：合法输出 YES，否则输出 NO。", tests: []testcase{{"([]{})\n", "YES\n"}, {"([)]\n", "NO\n"}}},
		{title: "最大连续子段", slug: "maximum-contiguous-sum", difficulty: 1, tags: []string{"array", "dynamic-programming"}, description: "在一个整数序列中选择一段非空连续区间，使区间元素之和最大。\n\n输入格式：第一行 n，第二行 n 个整数。\n输出格式：最大区间和。", tests: []testcase{{"9\n-2 1 -3 4 -1 2 1 -5 4\n", "6\n"}, {"4\n-8 -3 -6 -2\n", "-2\n"}}},
		{title: "合并重叠区间", slug: "merge-overlapping-intervals", difficulty: 2, tags: []string{"sorting", "interval"}, description: "给定若干闭区间，将有公共点的区间不断合并，输出最终互不重叠的区间。\n\n输入格式：第一行 n，之后 n 行各含左右端点 l、r。\n输出格式：先输出合并后的区间数，再按左端点递增逐行输出。", tests: []testcase{{"4\n1 3\n2 6\n8 10\n15 18\n", "3\n1 6\n8 10\n15 18\n"}, {"3\n1 4\n4 5\n9 9\n", "2\n1 5\n9 9\n"}}},
		{title: "网格中的最短步数", slug: "grid-shortest-steps", difficulty: 2, tags: []string{"breadth-first-search", "graph"}, description: "给定由 0 和 1 组成的网格，0 表示可通行，1 表示障碍。从左上角出发，每步可向上下左右移动，求到右下角的最少步数。起点和终点保证可通行。\n\n输入格式：第一行 h、w，之后 h 行每行含 w 个字符。\n输出格式：不可达输出 -1，否则输出最少步数。", tests: []testcase{{"3 4\n0000\n1100\n0000\n", "5\n"}, {"3 3\n010\n111\n000\n", "-1\n"}}},
		{title: "单词频次排序", slug: "word-frequency-order", difficulty: 1, tags: []string{"hash-table", "sorting", "string"}, description: "统计一行小写英文单词中每个不同单词的出现次数。单词之间由单个空格分隔。\n\n输入格式：一行文本。\n输出格式：按出现次数降序输出单词和次数；次数相同时按单词字典序升序。", tests: []testcase{{"pear apple pear banana apple pear\n", "pear 3\napple 2\nbanana 1\n"}, {"a c b c a b\n", "a 2\nb 2\nc 2\n"}}},
		{title: "最长严格递增子序列", slug: "longest-increasing-subsequence", difficulty: 2, tags: []string{"dynamic-programming", "binary-search"}, description: "给定一个整数序列，选择若干保持原相对顺序的元素，使它们严格递增。求最多能选择多少个元素。\n\n输入格式：第一行 n，第二行 n 个整数。\n输出格式：最长长度。", tests: []testcase{{"8\n10 9 2 5 3 7 101 18\n", "4\n"}, {"6\n1 2 3 4 5 6\n", "6\n"}}},
		{title: "有限容量装载", slug: "zero-one-capacity-pack", difficulty: 2, tags: []string{"dynamic-programming", "knapsack"}, description: "有 n 件物品，每件最多选择一次。第 i 件物品占用 wi 容量并产生 vi 价值，背包容量为 C，求最大总价值。\n\n输入格式：第一行 n、C，之后 n 行为 wi、vi。\n输出格式：最大总价值。", tests: []testcase{{"3 4\n2 3\n1 2\n3 4\n", "6\n"}, {"4 7\n1 1\n3 4\n4 5\n5 7\n", "9\n"}}},
		{title: "非负权单源最短路", slug: "nonnegative-single-source-shortest-path", difficulty: 2, tags: []string{"graph", "shortest-path", "priority-queue"}, description: "给定一张带非负整数权值的有向图，计算起点 s 到终点 t 的最短距离。\n\n输入格式：第一行 n、m、s、t，之后 m 行为边 u、v、w。\n输出格式：不可达输出 -1，否则输出最短距离。", tests: []testcase{{"4 5 1 4\n1 2 2\n1 3 5\n2 3 1\n2 4 7\n3 4 1\n", "4\n"}, {"3 1 1 3\n1 2 6\n", "-1\n"}}},
		{title: "任务依赖排序", slug: "dependency-topological-order", difficulty: 2, tags: []string{"graph", "topological-sort"}, description: "有 n 个任务和 m 条依赖。依赖 u v 表示必须先完成 u 再完成 v。输出任意一种可行的任务顺序。若存在循环依赖则输出 IMPOSSIBLE。\n\n输入格式：第一行 n、m，之后 m 行为 u、v。\n输出格式：可行时输出 n 个任务编号；评测器接受字典序最小的可行顺序。", tests: []testcase{{"4 3\n1 2\n1 3\n3 4\n", "1 2 3 4\n"}, {"2 2\n1 2\n2 1\n", "IMPOSSIBLE\n"}}},
		{title: "动态图连通查询", slug: "dynamic-connectivity-queries", difficulty: 2, tags: []string{"disjoint-set-union", "graph"}, description: "维护 n 个初始互不相连的点。操作 U a b 连接两个点所在的集合；操作 Q a b 查询两点当前是否连通。\n\n输入格式：第一行 n、q，之后 q 行为一条操作。\n输出格式：每个 Q 操作输出 YES 或 NO。", tests: []testcase{{"5 5\nQ 1 2\nU 1 2\nQ 1 2\nU 2 3\nQ 1 3\n", "NO\nYES\nYES\n"}, {"3 3\nU 1 3\nQ 2 3\nQ 1 3\n", "NO\nYES\n"}}},
		{title: "区间最小值查询", slug: "static-range-minimum-query", difficulty: 3, tags: []string{"segment-tree", "range-query"}, description: "给定一个不会修改的整数数组，回答多次闭区间最小值查询。\n\n输入格式：第一行 n、q，第二行 n 个整数，之后 q 行各为 l、r。位置使用 1-based 编号。\n输出格式：每次查询输出对应区间的最小值。", tests: []testcase{{"6 3\n5 2 7 1 3 4\n1 3\n2 5\n4 6\n", "2\n1\n1\n"}, {"4 2\n-1 -5 2 0\n1 4\n3 4\n", "-5\n0\n"}}},
	}
}
