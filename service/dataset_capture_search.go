package service

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/datasetcapture"
)

var ErrDatasetCaptureContentSearchTooBroad = errors.New("dataset capture content search is too broad; narrow the metadata filters")

const maxDatasetCaptureContentCandidates = 5000

func MatchDatasetCaptureContent(pathTemplate, node string, filter model.DatasetCaptureFilter, keyword string) ([]string, error) {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return nil, nil
	}
	filter.CaptureIDs = nil
	filter.NoMatches = false
	candidates, err := model.ListDatasetCaptureCandidates(filter, maxDatasetCaptureContentCandidates+1)
	if err != nil {
		return nil, err
	}
	if len(candidates) > maxDatasetCaptureContentCandidates {
		return nil, ErrDatasetCaptureContentSearchTooBroad
	}
	locators := make([]datasetcapture.RecordLocator, 0, len(candidates))
	for _, candidate := range candidates {
		locators = append(locators, datasetcapture.RecordLocator{
			Key: candidate.CaptureID, FileID: candidate.FileID, Row: candidate.Row,
		})
	}
	records, err := datasetcapture.NewBrowser(pathTemplate, node).ReadRecords(locators)
	if err != nil {
		return nil, err
	}
	matches := make([]string, 0)
	for _, candidate := range candidates {
		if strings.Contains(strings.ToLower(string(records[candidate.CaptureID])), keyword) {
			matches = append(matches, candidate.CaptureID)
		}
	}
	return matches, nil
}
