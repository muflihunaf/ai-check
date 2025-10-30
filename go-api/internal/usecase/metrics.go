package usecase

import "context"

// MetricsSummary represents aggregated verification insights.
type MetricsSummary struct {
	TotalRequests              int64   `json:"total_requests"`
	SuccessfulRequests         int64   `json:"successful_requests"`
	SuccessRate                float64 `json:"success_rate"`
	AverageScore               float64 `json:"average_score"`
	AverageProcessingLatencyMs float64 `json:"average_processing_latency_ms"`
}

// GetMetricsSummary aggregates verification metrics from persisted logs.
func (uc *VerificationUseCase) GetMetricsSummary(ctx context.Context) (*MetricsSummary, error) {
	aggregation, err := uc.repo.AggregateMetrics(ctx)
	if err != nil {
		return nil, err
	}

	summary := &MetricsSummary{
		TotalRequests:              aggregation.TotalCount,
		SuccessfulRequests:         aggregation.SuccessCount,
		AverageScore:               aggregation.AverageScore,
		AverageProcessingLatencyMs: aggregation.AverageProcessingLatencyMs,
	}

	if aggregation.TotalCount > 0 {
		summary.SuccessRate = float64(aggregation.SuccessCount) / float64(aggregation.TotalCount)
	}

	return summary, nil
}
