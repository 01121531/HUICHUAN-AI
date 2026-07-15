package authz

const (
	ResourceDatasetCapture = "dataset_capture"

	ActionView     = "view"
	ActionDownload = "download"
)

var (
	DatasetCaptureView     = Permission{Resource: ResourceDatasetCapture, Action: ActionView}
	DatasetCaptureDownload = Permission{Resource: ResourceDatasetCapture, Action: ActionDownload}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceDatasetCapture,
		LabelKey: "Dataset Capture",
		Actions: []ActionDefinition{
			{
				Action:         ActionView,
				LabelKey:       "View dataset captures",
				DescriptionKey: "Browse captured users, records, and complete request and response details.",
			},
			{
				Action:         ActionDownload,
				LabelKey:       "Download dataset captures",
				DescriptionKey: "Export selected capture records as training JSONL data.",
			},
		},
	})
}
