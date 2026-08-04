package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
)

const (
	defaultProxyHealthCheckURL            = "https://api64.ipify.org?format=json"
	defaultProxyHealthCheckTimeoutSeconds = 10
	defaultProxyHealthCheckConcurrency    = 8
	defaultProxyHealthCheckBatchSize      = 100
)

type ProxyHealthCheckSummary struct {
	Checked     int `json:"checked"`
	Healthy     int `json:"healthy"`
	Failed      int `json:"failed"`
	Unavailable int `json:"unavailable"`
	Recovering  int `json:"recovering"`
	Switched    int `json:"switched"`
}

type proxyHealthCheckOutcome struct {
	Success       bool
	LatencyMs     int
	ExitIp        string
	FailureReason string
}

type ProxyHealthCheckResult struct {
	ProxyId       int    `json:"proxy_id"`
	Success       bool   `json:"success"`
	LatencyMs     int    `json:"latency_ms"`
	ExitIp        string `json:"exit_ip"`
	FailureReason string `json:"failure_reason"`
	Status        string `json:"status"`
}

func CheckManagedProxyNow(ctx context.Context, proxyId int) (ProxyHealthCheckResult, error) {
	result := ProxyHealthCheckResult{ProxyId: proxyId}
	target, err := model.GetProxyHealthTarget(proxyId)
	if err != nil {
		return result, err
	}
	outcome := checkManagedProxyHealth(ctx, target.Proxy)
	transition, err := model.ApplyProxyHealthCheckResult(proxyId, outcome.Success, outcome.LatencyMs, outcome.ExitIp, outcome.FailureReason)
	if err != nil {
		return result, err
	}
	if transition.SwitchRequired {
		if _, err := switchManagedProxyGroup(ctx, transition.ProxyGroupId, transition.ProxyId, transition.SwitchWaitSeconds); err != nil {
			return result, err
		}
	}
	if transition.ProbeRequired {
		if err := switchManagedProxyGroupTo(ctx, transition.ProxyGroupId, transition.ProxyId, transition.SwitchWaitSeconds); err != nil {
			return result, err
		}
	}
	result.Success = outcome.Success
	result.LatencyMs = outcome.LatencyMs
	result.ExitIp = outcome.ExitIp
	result.FailureReason = outcome.FailureReason
	result.Status = transition.ToStatus
	return result, nil
}

func RunProxyHealthCheckTask(ctx context.Context) (ProxyHealthCheckSummary, error) {
	summary := ProxyHealthCheckSummary{}
	targets, err := model.ListDueProxyHealthChecks(common.GetTimestamp(), defaultProxyHealthCheckBatchSize)
	if err != nil {
		return summary, err
	}
	if len(targets) == 0 {
		return summary, nil
	}
	workerCount := min(defaultProxyHealthCheckConcurrency, len(targets))
	jobs := make(chan *model.ProxyHealthCheckTarget)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	worker := func() {
		defer wg.Done()
		for target := range jobs {
			if ctx.Err() != nil {
				return
			}
			outcome := checkManagedProxyHealth(ctx, target.Proxy)
			transition, applyErr := model.ApplyProxyHealthCheckResult(
				target.Proxy.Id, outcome.Success, outcome.LatencyMs, outcome.ExitIp, outcome.FailureReason,
			)
			if applyErr == nil && transition.SwitchRequired {
				var nextProxyId int
				nextProxyId, applyErr = switchManagedProxyGroup(ctx, transition.ProxyGroupId, transition.ProxyId, transition.SwitchWaitSeconds)
				if nextProxyId > 0 {
					mu.Lock()
					summary.Switched++
					mu.Unlock()
				}
			}
			if applyErr == nil && transition.ProbeRequired {
				applyErr = switchManagedProxyGroupTo(ctx, transition.ProxyGroupId, transition.ProxyId, transition.SwitchWaitSeconds)
			}

			mu.Lock()
			summary.Checked++
			if outcome.Success {
				summary.Healthy++
			} else {
				summary.Failed++
			}
			if transition.ToStatus == model.ProxyStatusUnavailable && transition.FromStatus != transition.ToStatus {
				summary.Unavailable++
			}
			if transition.ToStatus == model.ProxyStatusRecovering && transition.FromStatus != transition.ToStatus {
				summary.Recovering++
			}
			if applyErr != nil && firstErr == nil {
				firstErr = applyErr
			}
			mu.Unlock()
		}
	}
	for index := 0; index < workerCount; index++ {
		wg.Add(1)
		go worker()
	}
	for _, target := range targets {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return summary, ctx.Err()
		case jobs <- target:
		}
	}
	close(jobs)
	wg.Wait()
	return summary, firstErr
}

func checkManagedProxyHealth(ctx context.Context, proxy *model.Proxy) proxyHealthCheckOutcome {
	startedAt := time.Now()
	outcome := proxyHealthCheckOutcome{}
	if proxy == nil {
		outcome.FailureReason = "invalid_proxy"
		return outcome
	}
	client, err := NewProxyHttpClient(proxy.URL())
	if err != nil {
		outcome.FailureReason = "client_init_failed"
		return outcome
	}
	timeoutSeconds := common.GetEnvOrDefault("PROXY_HEALTH_CHECK_TIMEOUT_SECONDS", defaultProxyHealthCheckTimeoutSeconds)
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultProxyHealthCheckTimeoutSeconds
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	url := strings.TrimSpace(common.GetEnvOrDefaultString("PROXY_HEALTH_CHECK_URL", defaultProxyHealthCheckURL))
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		outcome.FailureReason = "invalid_check_url"
		return outcome
	}
	checkClient := *client
	checkClient.Timeout = time.Duration(timeoutSeconds) * time.Second
	response, err := checkClient.Do(request)
	outcome.LatencyMs = int(time.Since(startedAt).Milliseconds())
	if err != nil {
		outcome.FailureReason = "request_failed"
		return outcome
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		outcome.FailureReason = fmt.Sprintf("http_status_%d", response.StatusCode)
		return outcome
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		outcome.FailureReason = "response_read_failed"
		return outcome
	}
	outcome.ExitIp = parseProxyHealthExitIP(body)
	if outcome.ExitIp == "" {
		outcome.FailureReason = "invalid_exit_ip"
		return outcome
	}
	if !proxyExpectedExitIPMatches(proxy.ExpectedExitIp, outcome.ExitIp) {
		outcome.FailureReason = "exit_ip_mismatch"
		return outcome
	}
	outcome.Success = true
	return outcome
}

func parseProxyHealthExitIP(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	var payload struct {
		IP string `json:"ip"`
	}
	if json.Unmarshal(body, &payload) == nil && net.ParseIP(strings.TrimSpace(payload.IP)) != nil {
		return strings.TrimSpace(payload.IP)
	}
	trimmed = strings.Trim(trimmed, `"`)
	if net.ParseIP(trimmed) != nil {
		return trimmed
	}
	return ""
}

func proxyExpectedExitIPMatches(expected string, actual string) bool {
	expected = strings.TrimSpace(expected)
	actualIP := net.ParseIP(strings.TrimSpace(actual))
	if expected == "" {
		return actualIP != nil
	}
	if actualIP == nil {
		return false
	}
	for _, item := range strings.Split(expected, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(item); err == nil {
			if network.Contains(actualIP) {
				return true
			}
			continue
		}
		if expectedIP := net.ParseIP(item); expectedIP != nil && expectedIP.Equal(actualIP) {
			return true
		}
	}
	return false
}

func ProxyHealthCheckTaskInterval() time.Duration { return 15 * time.Second }
