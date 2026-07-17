package controller

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/xuri/excelize/v2"
)

func syntheticExportLog(t *testing.T) *model.Log {
	t.Helper()
	snapshot := model.BillingSnapshotV1{
		Version:           model.BillingSnapshotVersion,
		Status:            "complete",
		Mode:              "per_token",
		Source:            "wallet",
		RequestedModel:    "gpt-test",
		EffectiveModel:    "gpt-test-effective",
		BaseCurrency:      "USD",
		QuotaPerUnit:      "500000",
		DisplayCurrency:   "USD",
		ExchangeRate:      "1",
		GroupRatio:        "1",
		Components:        []model.BillingComponent{{Kind: "input_tokens", Quantity: 10, Unit: "token", UnitPriceUSD: "2", PriceUnit: 1000000, Ratio: "1", SubtotalQuota: 10, SubtotalQuotaExact: "10"}},
		FinalChargedQuota: 10,
		Rounding:          "round_half_away_from_zero",
	}
	other, err := json.Marshal(map[string]interface{}{
		"billing_snapshot_v1": snapshot,
		"upstream_model_name": "gpt-test-effective",
		"request_path":        "/v1/chat/completions",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &model.Log{
		Id:                1,
		UserId:            2,
		CreatedAt:         time.Date(2026, 7, 17, 12, 0, 0, 0, time.Local).Unix(),
		Type:              model.LogTypeConsume,
		Content:           "=unsafe formula, \"quoted\"\nnext",
		Username:          "测试用户",
		TokenName:         "token-a",
		ModelName:         "gpt-test",
		Quota:             10,
		PromptTokens:      10,
		CompletionTokens:  2,
		UseTime:           3,
		IsStream:          true,
		ChannelId:         4,
		ChannelName:       "渠道A",
		TokenId:           5,
		Group:             "default",
		Ip:                "192.168.31.102",
		RequestId:         "=request-id",
		UpstreamRequestId: "upstream-id",
		Other:             string(other),
	}
}

func TestWriteUsageLogCSVFullIPAndFormulaDefense(t *testing.T) {
	var output bytes.Buffer
	if err := writeUsageLogCSV(&output, []*model.Log{syntheticExportLog(t)}); err != nil {
		t.Fatal(err)
	}
	raw := output.Bytes()
	if len(raw) < 3 || !bytes.Equal(raw[:3], []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("CSV does not have UTF-8 BOM")
	}
	reader := csv.NewReader(bytes.NewReader(raw[3:]))
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("CSV is not RFC 4180 parseable: %v", err)
	}
	if len(rows) != 2 || len(rows[1]) != len(usageLogExportHeaders) {
		t.Fatalf("unexpected CSV shape: %d rows, %d columns", len(rows), len(rows[1]))
	}
	if rows[1][11] != "192.168.31.102" {
		t.Fatalf("export did not preserve full IP: %q", rows[1][11])
	}
	if rows[1][12] != "'=request-id" || !strings.HasPrefix(rows[1][23], "'=") {
		t.Fatalf("formula injection defense failed: request=%q content=%q", rows[1][12], rows[1][23])
	}
}

func TestWriteUsageLogXLSXCanBeParsed(t *testing.T) {
	var output bytes.Buffer
	request := usageLogExportRequest{SelectionMode: "selected", Format: "xlsx", IPVisibility: "full"}
	if err := writeUsageLogXLSX(&output, []*model.Log{syntheticExportLog(t)}, request); err != nil {
		t.Fatal(err)
	}
	workbook, err := excelize.OpenReader(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatalf("generated XLSX cannot be parsed: %v", err)
	}
	defer workbook.Close()
	for _, sheet := range []string{"日志明细", "计价组成", "导出说明"} {
		if index, err := workbook.GetSheetIndex(sheet); err != nil || index == -1 {
			t.Fatalf("missing sheet %q: index=%d err=%v", sheet, index, err)
		}
	}
	rows, err := workbook.GetRows("日志明细")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1][11] != "192.168.31.102" {
		t.Fatalf("unexpected XLSX details rows: %#v", rows)
	}
	components, err := workbook.GetRows("计价组成")
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 2 || components[1][1] != "input_tokens" {
		t.Fatalf("billing component sheet is invalid: %#v", components)
	}
}

func TestNormalizeUsageLogExportRequest(t *testing.T) {
	request := usageLogExportRequest{
		SelectionMode: "selected",
		Format:        "CSV",
		IPVisibility:  "anything",
		IDs:           []int{2, 2, -1, 3},
		RequestIDs:    []string{"a", "", "a", "b"},
	}
	if err := normalizeUsageLogExportRequest(&request); err != nil {
		t.Fatal(err)
	}
	if request.Format != "csv" || request.IPVisibility != "full" {
		t.Fatalf("normalization failed: %+v", request)
	}
	if len(request.Filters.SelectedIDs) != 2 || len(request.Filters.SelectedRequestIDs) != 2 {
		t.Fatalf("selection was not deduplicated: %+v", request.Filters)
	}
}
