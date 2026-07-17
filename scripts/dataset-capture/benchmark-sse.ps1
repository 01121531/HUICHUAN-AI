param(
    [Parameter(Mandatory = $true)]
    [string]$ApiKey,
    [string]$Model = "gpt-4o-mini",
    [string]$DisabledBaseUrl = "http://localhost:3102",
    [string]$EnabledBaseUrl = "http://localhost:3102",
    [int]$DisabledProcessId = 0,
    [int]$EnabledProcessId = 0,
    [int]$Runs = 20,
    [int]$Warmup = 2,
    [string]$OutputPath = ".run/dataset-capture-sse-benchmark.json"
)

$ErrorActionPreference = "Stop"

function Get-Percentile {
    param([double[]]$Values, [int]$Percent)
    if ($Values.Count -eq 0) { return 0 }
    $sorted = @($Values | Sort-Object)
    $index = [Math]::Ceiling($sorted.Count * $Percent / 100.0) - 1
    return [Math]::Round($sorted[[Math]::Max(0, $index)], 3)
}

function Get-ProcessSnapshot {
    param([int]$TargetProcessId)
    if ($TargetProcessId -le 0) { return $null }
    $process = Get-Process -Id $TargetProcessId -ErrorAction SilentlyContinue
    if ($null -eq $process) { return $null }
    return [ordered]@{
        process_id = $TargetProcessId
        cpu_seconds = [Math]::Round($process.CPU, 3)
        working_set_mb = [Math]::Round($process.WorkingSet64 / 1MB, 3)
        private_memory_mb = [Math]::Round($process.PrivateMemorySize64 / 1MB, 3)
    }
}

function Invoke-StreamingRequest {
    param([System.Net.Http.HttpClient]$Client, [string]$BaseUrl)
    $payload = @{
        model = $Model
        stream = $true
        messages = @(@{ role = "user"; content = "Reply with exactly: benchmark-ok" })
    } | ConvertTo-Json -Depth 8 -Compress
    $request = [System.Net.Http.HttpRequestMessage]::new(
        [System.Net.Http.HttpMethod]::Post,
        ($BaseUrl.TrimEnd("/") + "/v1/chat/completions")
    )
    $request.Content = [System.Net.Http.StringContent]::new($payload, [Text.Encoding]::UTF8, "application/json")
    $watch = [Diagnostics.Stopwatch]::StartNew()
    $response = $null
    $stream = $null
    try {
        $response = $Client.SendAsync(
            $request,
            [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead
        ).GetAwaiter().GetResult()
        $stream = $response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
        $buffer = New-Object byte[] 16384
        $firstByteMs = $null
        $lastChunkMs = $null
        $chunkIntervals = @()
        $bytes = 0L
        do {
            $read = $stream.ReadAsync($buffer, 0, $buffer.Length).GetAwaiter().GetResult()
            if ($read -gt 0) {
                $nowMs = $watch.Elapsed.TotalMilliseconds
                if ($null -eq $firstByteMs) {
                    $firstByteMs = $nowMs
                }
                elseif ($null -ne $lastChunkMs) {
                    $chunkIntervals += [Math]::Round($nowMs - $lastChunkMs, 3)
                }
                $lastChunkMs = $nowMs
                $bytes += $read
            }
        } while ($read -gt 0)
        $watch.Stop()
        return [pscustomobject]@{
            success = $response.IsSuccessStatusCode -and $bytes -gt 0
            status = [int]$response.StatusCode
            first_byte_ms = [Math]::Round([double]$firstByteMs, 3)
            total_ms = [Math]::Round($watch.Elapsed.TotalMilliseconds, 3)
            bytes = $bytes
            chunk_intervals_ms = $chunkIntervals
        }
    }
    finally {
        if ($null -ne $stream) { $stream.Dispose() }
        if ($null -ne $response) { $response.Dispose() }
        $request.Dispose()
    }
}

function Invoke-Suite {
    param(
        [string]$Name,
        [string]$BaseUrl,
        [int]$TargetProcessId,
        [System.Net.Http.HttpClient]$Client
    )
    for ($index = 0; $index -lt $Warmup; $index++) {
        [void](Invoke-StreamingRequest -Client $Client -BaseUrl $BaseUrl)
    }
    $resourceBefore = Get-ProcessSnapshot $TargetProcessId
    $samples = @()
    for ($index = 0; $index -lt $Runs; $index++) {
        $samples += Invoke-StreamingRequest -Client $Client -BaseUrl $BaseUrl
    }
    $resourceAfter = Get-ProcessSnapshot $TargetProcessId
    $successful = @($samples | Where-Object success)
    $chunkIntervals = @($successful | ForEach-Object { $_.chunk_intervals_ms })
    $resources = $null
    if ($null -ne $resourceBefore -and $null -ne $resourceAfter) {
        $resources = [ordered]@{
            process_id = $TargetProcessId
            cpu_seconds_delta = [Math]::Round(
                $resourceAfter.cpu_seconds - $resourceBefore.cpu_seconds,
                3
            )
            working_set_mb_before = $resourceBefore.working_set_mb
            working_set_mb_after = $resourceAfter.working_set_mb
            working_set_mb_delta = [Math]::Round(
                $resourceAfter.working_set_mb - $resourceBefore.working_set_mb,
                3
            )
            private_memory_mb_before = $resourceBefore.private_memory_mb
            private_memory_mb_after = $resourceAfter.private_memory_mb
            private_memory_mb_delta = [Math]::Round(
                $resourceAfter.private_memory_mb - $resourceBefore.private_memory_mb,
                3
            )
        }
    }
    return [ordered]@{
        name = $Name
        base_url = $BaseUrl
        runs = $Runs
        successful = $successful.Count
        first_byte_ms = [ordered]@{
            p50 = Get-Percentile $successful.first_byte_ms 50
            p95 = Get-Percentile $successful.first_byte_ms 95
            p99 = Get-Percentile $successful.first_byte_ms 99
        }
        total_ms = [ordered]@{
            p50 = Get-Percentile $successful.total_ms 50
            p95 = Get-Percentile $successful.total_ms 95
            p99 = Get-Percentile $successful.total_ms 99
        }
        chunk_interval_ms = [ordered]@{
            samples = $chunkIntervals.Count
            p50 = Get-Percentile $chunkIntervals 50
            p95 = Get-Percentile $chunkIntervals 95
            p99 = Get-Percentile $chunkIntervals 99
        }
        response_bytes = [long](($successful | Measure-Object bytes -Sum).Sum)
        resources = $resources
    }
}

if ($Runs -lt 1 -or $Warmup -lt 0) { throw "Runs must be positive and Warmup cannot be negative" }
Add-Type -AssemblyName System.Net.Http
$handler = [System.Net.Http.HttpClientHandler]::new()
$client = [System.Net.Http.HttpClient]::new($handler)
$client.Timeout = [TimeSpan]::FromMinutes(10)
$client.DefaultRequestHeaders.Authorization = [System.Net.Http.Headers.AuthenticationHeaderValue]::new("Bearer", $ApiKey)
try {
    $disabled = Invoke-Suite "capture-disabled" $DisabledBaseUrl $DisabledProcessId $client
    $enabled = Invoke-Suite "capture-enabled" $EnabledBaseUrl $EnabledProcessId $client
    $report = [ordered]@{
        generated_at = [DateTimeOffset]::UtcNow.ToString("o")
        model = $Model
        note = "Use two otherwise-identical instances with capture disabled/enabled for a valid A/B comparison. API keys are never written."
        disabled = $disabled
        enabled = $enabled
        p95_delta = [ordered]@{
            first_byte_ms = [Math]::Round($enabled.first_byte_ms.p95 - $disabled.first_byte_ms.p95, 3)
            chunk_interval_ms = [Math]::Round(
                $enabled.chunk_interval_ms.p95 - $disabled.chunk_interval_ms.p95,
                3
            )
            total_ms = [Math]::Round($enabled.total_ms.p95 - $disabled.total_ms.p95, 3)
        }
    }
    $absoluteOutput = [IO.Path]::GetFullPath((Join-Path (Get-Location) $OutputPath))
    [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($absoluteOutput)) | Out-Null
    $report | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $absoluteOutput -Encoding UTF8
    $report | ConvertTo-Json -Depth 10
}
finally {
    $client.Dispose()
    $handler.Dispose()
}
