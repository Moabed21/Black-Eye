package alerts

import (
	"testing"

	"blackeye/internal/config"
)

func TestEvaluateCustomRule(t *testing.T) {
	ruleGt := config.CustomRule{Metric: "cpu", Operator: ">", Value: 75.0}
	if !EvaluateCustomRule(ruleGt, 80.0) {
		t.Fatalf("expected 80 > 75 to evaluate true")
	}
	if EvaluateCustomRule(ruleGt, 70.0) {
		t.Fatalf("expected 70 > 75 to evaluate false")
	}

	ruleGte := config.CustomRule{Metric: "ram", Operator: ">=", Value: 85.0}
	if !EvaluateCustomRule(ruleGte, 85.0) {
		t.Fatalf("expected 85 >= 85 to evaluate true")
	}

	ruleLt := config.CustomRule{Metric: "temp", Operator: "<", Value: 40.0}
	if !EvaluateCustomRule(ruleLt, 35.0) {
		t.Fatalf("expected 35 < 40 to evaluate true")
	}

	ruleEq := config.CustomRule{Metric: "auth_failures", Operator: "==", Value: 5.0}
	if !EvaluateCustomRule(ruleEq, 5.0) {
		t.Fatalf("expected 5 == 5 to evaluate true")
	}
}

func TestCustomRuleValidation(t *testing.T) {
	validRule := config.CustomRule{Metric: "cpu", Operator: ">", Value: 80.0}
	if err := validRule.Validate(); err != nil {
		t.Fatalf("expected valid rule, got error: %v", err)
	}

	outOfBoundsRule := config.CustomRule{Metric: "cpu", Operator: ">", Value: 150.0}
	if err := outOfBoundsRule.Validate(); err == nil {
		t.Fatalf("expected out of bounds error for 150%% CPU threshold")
	}

	invalidMetricRule := config.CustomRule{Metric: "invalid_metric", Operator: ">", Value: 50.0}
	if err := invalidMetricRule.Validate(); err == nil {
		t.Fatalf("expected error for invalid metric name")
	}
}
