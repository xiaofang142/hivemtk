// Package perflib 提供性能压测工具集
// 独立部署版本：单租户，所有压测目标都针对 user-server
package perflib

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Result 单次压测结果
type Result struct {
	Name          string        `json:"name"`
	URL           string        `json:"url"`
	Method        string        `json:"method"`
	TotalRequests int           `json:"total_requests"`
	Concurrency   int           `json:"concurrency"`
	Duration      time.Duration `json:"duration"`
	SuccessCount  int64         `json:"success_count"`
	FailedCount   int64         `json:"failed_count"`
	Throughput    float64       `json:"throughput"`
	LatencyAvg    time.Duration `json:"latency_avg"`
	LatencyP50    time.Duration `json:"latency_p50"`
	LatencyP95    time.Duration `json:"latency_p95"`
	LatencyP99    time.Duration `json:"latency_p99"`
	LatencyMax    time.Duration `json:"latency_max"`
	LatencyMin    time.Duration `json:"latency_min"`
	StatusCodes   map[int]int64 `json:"status_codes"`
}

// Config 压测配置
type Config struct {
	Name        string
	URL         string
	Method      string
	Headers     map[string]string
	Body        any
	Concurrency int
	Total       int
	Timeout     time.Duration
}

// LoadRunner 压测执行器
type LoadRunner struct {
	client *http.Client
}

// NewLoadRunner 创建压测执行器
func NewLoadRunner() *LoadRunner {
	return &LoadRunner{
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        200,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// Run 执行压测
func (lr *LoadRunner) Run(ctx context.Context, cfg Config) (*Result, error) {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 10
	}
	if cfg.Total <= 0 {
		cfg.Total = 1000
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.Method == "" {
		cfg.Method = http.MethodGet
	}

	var (
		successCount int64
		failedCount  int64
		latencies    = make([]time.Duration, 0, cfg.Total)
		statusCodes  = make(map[int]int64)
		mu           sync.Mutex
	)

	var bodyReader io.Reader
	if cfg.Body != nil {
		b, err := json.Marshal(cfg.Body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	startTime := time.Now()
	wg := sync.WaitGroup{}
	jobs := make(chan int, cfg.Total)
	for i := 0; i < cfg.Total; i++ {
		jobs <- i
	}
	close(jobs)

	for w := 0; w < cfg.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				reqStart := time.Now()

				req, err := http.NewRequestWithContext(ctx, cfg.Method, cfg.URL, bodyReader)
				if err != nil {
					atomic.AddInt64(&failedCount, 1)
					continue
				}
				for k, v := range cfg.Headers {
					req.Header.Set(k, v)
				}
				if cfg.Body != nil && req.Header.Get("Content-Type") == "" {
					req.Header.Set("Content-Type", "application/json")
				}

				client := &http.Client{Timeout: cfg.Timeout}
				resp, err := client.Do(req)
				latency := time.Since(reqStart)

				if err != nil {
					atomic.AddInt64(&failedCount, 1)
					mu.Lock()
					latencies = append(latencies, latency)
					mu.Unlock()
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()

				mu.Lock()
				statusCodes[resp.StatusCode]++
				mu.Unlock()
				if resp.StatusCode >= 200 && resp.StatusCode < 400 {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&failedCount, 1)
				}

				mu.Lock()
				latencies = append(latencies, latency)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	duration := time.Since(startTime)

	result := &Result{
		Name:          cfg.Name,
		URL:           cfg.URL,
		Method:        cfg.Method,
		TotalRequests: cfg.Total,
		Concurrency:   cfg.Concurrency,
		Duration:      duration,
		SuccessCount:  successCount,
		FailedCount:   failedCount,
		StatusCodes:   statusCodes,
	}

	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		result.LatencyMin = latencies[0]
		result.LatencyMax = latencies[len(latencies)-1]

		var sum time.Duration
		for _, l := range latencies {
			sum += l
		}
		result.LatencyAvg = sum / time.Duration(len(latencies))
		result.LatencyP50 = latencies[len(latencies)*50/100]
		result.LatencyP95 = latencies[len(latencies)*95/100]
		result.LatencyP99 = latencies[len(latencies)*99/100]
	}

	if duration > 0 {
		result.Throughput = float64(cfg.Total) / duration.Seconds()
	}

	return result, nil
}

// PrintResult 打印压测结果
func PrintResult(r *Result) {
	fmt.Println("=" + repeat("=", 78))
	fmt.Printf("📊 压测结果: %s\n", r.Name)
	fmt.Println(repeat("=", 79))
	fmt.Printf("目标 URL:       %s %s\n", r.Method, r.URL)
	fmt.Printf("总请求数:       %d\n", r.TotalRequests)
	fmt.Printf("并发数:         %d\n", r.Concurrency)
	fmt.Printf("总耗时:         %s\n", r.Duration.Round(time.Millisecond))
	fmt.Printf("成功数:         %d\n", r.SuccessCount)
	fmt.Printf("失败数:         %d\n", r.FailedCount)
	fmt.Printf("吞吐 (QPS):     %.2f\n", r.Throughput)
	fmt.Println("--- 延迟分布 ---")
	fmt.Printf("Min:            %s\n", r.LatencyMin.Round(time.Millisecond))
	fmt.Printf("Avg:            %s\n", r.LatencyAvg.Round(time.Millisecond))
	fmt.Printf("P50:            %s\n", r.LatencyP50.Round(time.Millisecond))
	fmt.Printf("P95:            %s\n", r.LatencyP95.Round(time.Millisecond))
	fmt.Printf("P99:            %s\n", r.LatencyP99.Round(time.Millisecond))
	fmt.Printf("Max:            %s\n", r.LatencyMax.Round(time.Millisecond))
	if len(r.StatusCodes) > 0 {
		fmt.Println("--- 状态码分布 ---")
		for code, count := range r.StatusCodes {
			fmt.Printf("  %d: %d\n", code, count)
		}
	}
	fmt.Println(repeat("=", 79))
}

func repeat(s string, n int) string {
	r := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		r = append(r, s...)
	}
	return string(r)
}
