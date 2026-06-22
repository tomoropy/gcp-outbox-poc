package jobs

import (
	"testing"
	"time"

	"github.com/tomoropy/gcp-outbox-poc/internal/spannerdb"
)

func TestOutboxAutoscalerJob_desiredWorkers(t *testing.T) {
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	oldestReadyAt := now.Add(-10 * time.Minute)

	tests := []struct {
		name           string
		policy         OutboxAutoscalerPolicy
		stats          spannerdb.OutboxBacklogStats
		currentWorkers int
		wantWorkers    int
		wantReason     string
	}{
		{
			name: "成功: idleなら最小台数に戻す",
			policy: OutboxAutoscalerPolicy{
				MinWorkers:       1,
				MaxWorkers:       5,
				ScaleUpBacklog:   100,
				ScaleUpWorkers:   3,
				MaxBacklog:       500,
				OldestBacklogAge: 5 * time.Minute,
			},
			stats:          spannerdb.OutboxBacklogStats{},
			currentWorkers: 3,
			wantWorkers:    1,
			wantReason:     "idle",
		},
		{
			name: "成功: backlogしきい値を超えたら指定台数まで増やす",
			policy: OutboxAutoscalerPolicy{
				MinWorkers:       1,
				MaxWorkers:       5,
				ScaleUpBacklog:   100,
				ScaleUpWorkers:   3,
				MaxBacklog:       500,
				OldestBacklogAge: 5 * time.Minute,
			},
			stats: spannerdb.OutboxBacklogStats{
				ReadyCount: 120,
			},
			currentWorkers: 1,
			wantWorkers:    3,
			wantReason:     "scale_up_backlog",
		},
		{
			name: "成功: max backlogしきい値を超えたら最大台数まで増やす",
			policy: OutboxAutoscalerPolicy{
				MinWorkers:       1,
				MaxWorkers:       5,
				ScaleUpBacklog:   100,
				ScaleUpWorkers:   3,
				MaxBacklog:       500,
				OldestBacklogAge: 5 * time.Minute,
			},
			stats: spannerdb.OutboxBacklogStats{
				ReadyCount: 600,
			},
			currentWorkers: 1,
			wantWorkers:    5,
			wantReason:     "scale_up_max_backlog",
		},
		{
			name: "成功: 古いbacklogがあれば1段階増やす",
			policy: OutboxAutoscalerPolicy{
				MinWorkers:       1,
				MaxWorkers:       5,
				ScaleUpBacklog:   100,
				ScaleUpWorkers:   3,
				MaxBacklog:       500,
				OldestBacklogAge: 5 * time.Minute,
			},
			stats: spannerdb.OutboxBacklogStats{
				ReadyCount:    1,
				OldestReadyAt: &oldestReadyAt,
			},
			currentWorkers: 1,
			wantWorkers:    2,
			wantReason:     "scale_up_oldest_age",
		},
		{
			name: "成功: 処理中があれば現在台数を維持する",
			policy: OutboxAutoscalerPolicy{
				MinWorkers:       1,
				MaxWorkers:       5,
				ScaleUpBacklog:   100,
				ScaleUpWorkers:   3,
				MaxBacklog:       500,
				OldestBacklogAge: 5 * time.Minute,
			},
			stats: spannerdb.OutboxBacklogStats{
				ProcessingCount: 3,
			},
			currentWorkers: 3,
			wantWorkers:    3,
			wantReason:     "keep_current",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			job := NewOutboxAutoscalerJob(nil, nil, tt.policy)
			gotWorkers, gotReason := job.desiredWorkers(tt.stats, tt.currentWorkers, now)
			if gotWorkers != tt.wantWorkers {
				t.Fatalf("workers = %d, want %d", gotWorkers, tt.wantWorkers)
			}
			if gotReason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", gotReason, tt.wantReason)
			}
		})
	}
}
