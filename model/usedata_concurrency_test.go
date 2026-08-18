package model

import (
	"reflect"
	"testing"
)

func TestCalculatePeakConcurrencyByHour(t *testing.T) {
	tests := []struct {
		name string
		logs []concurrencyLogInterval
		want map[int64]int
	}{
		{
			name: "overlapping requests",
			logs: []concurrencyLogInterval{
				{CreatedAt: 100, UseTime: 10},
				{CreatedAt: 105, UseTime: 10},
			},
			want: map[int64]int{0: 2},
		},
		{
			name: "request spanning hour boundary",
			logs: []concurrencyLogInterval{
				{CreatedAt: 3610, UseTime: 20},
				{CreatedAt: 3605, UseTime: 10},
			},
			want: map[int64]int{0: 2, 3600: 2},
		},
		{
			name: "touching requests do not overlap",
			logs: []concurrencyLogInterval{
				{CreatedAt: 3600, UseTime: 10},
				{CreatedAt: 3610, UseTime: 10},
			},
			want: map[int64]int{0: 1, 3600: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculatePeakConcurrencyByHour(tt.logs, 0, 7200)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("calculatePeakConcurrencyByHour() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestAttachConcurrencyToQuotaDataAssignsEachHourOnce(t *testing.T) {
	data := []*QuotaData{
		{CreatedAt: 3600, ModelName: "first"},
		{CreatedAt: 3600, ModelName: "second"},
		{CreatedAt: 7200, ModelName: "third"},
	}

	AttachConcurrencyToQuotaData(data, map[int64]int{3600: 4, 7200: 2})

	if data[0].Concurrency != 4 || data[1].Concurrency != 0 || data[2].Concurrency != 2 {
		t.Fatalf("unexpected concurrency assignment: %#v", data)
	}
}
