package domain

import (
	"local-music-queue/internal/domain/entity"
	"testing"
)

func TestAutoQueueConfig_Defaults(t *testing.T) {
	cfg := AutoQueueConfig{
		Enabled:  false,
		Strategy: StrategyRelated,
	}

	if cfg.Enabled {
		t.Error("expected Enabled to be false by default")
	}
	if cfg.Strategy != StrategyRelated {
		t.Errorf("expected Strategy to be 'related', got %s", cfg.Strategy)
	}
}

func TestAutoQueueStrategy_Constants(t *testing.T) {
	if StrategyRelated != "related" {
		t.Errorf("expected StrategyRelated='related', got %s", StrategyRelated)
	}
	if StrategyHistoryRandom != "history_random" {
		t.Errorf("expected StrategyHistoryRandom='history_random', got %s", StrategyHistoryRandom)
	}
}

func TestSystemUserID_Constant(t *testing.T) {
	if entity.SystemUserID != "system:autoqueue" {
		t.Errorf("expected SystemUserID='system:autoqueue', got %s", entity.SystemUserID)
	}
}
