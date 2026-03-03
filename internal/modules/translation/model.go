package translation

// AnalyzeRequest is the message consumed from "file.analyze".
type AnalyzeRequest struct {
	FileID        int64  `json:"file_id"`
	ObjectKey     string `json:"object_key"`
	ContentType   string `json:"content_type"`
	CorrelationID string `json:"correlation_id"`
}

// AnalysisReply is the message published to "file.analysis.result".
type AnalysisReply struct {
	FileID             int64  `json:"file_id"`
	TranslationSummary string `json:"translation_summary"`
	CorrelationID      string `json:"correlation_id"`
	Error              string `json:"error"`
}
