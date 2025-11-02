package main

import (
	"testing"
)

func TestIsExcludedEmail(t *testing.T) {
	excludedEmails := []string{
		"no-reply@accounts.google.com",
		"noreply@google.com",
		"mail-noreply@google.com",
	}

	tests := []struct {
		email    string
		expected bool
		name     string
	}{
		{
			email:    "no-reply@accounts.google.com",
			expected: true,
			name:     "exact match",
		},
		{
			email:    "user@example.com",
			expected: false,
			name:     "regular email",
		},
		{
			email:    "NOREPLY@GOOGLE.COM",
			expected: true,
			name:     "case insensitive match",
		},
		{
			email:    "",
			expected: false,
			name:     "empty email",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isExcludedEmail(tt.email, excludedEmails)
			if result != tt.expected {
				t.Errorf("isExcludedEmail(%s) = %v, want %v", tt.email, result, tt.expected)
			}
		})
	}
}

// Test utility functions by creating helper functions
func selectOperatorFromCounts(operators []string, taskCounts map[string]int) string {
	if len(operators) == 0 {
		return ""
	}

	minCount := -1
	selectedOperator := ""
	for _, operator := range operators {
		count := taskCounts[operator] // defaults to 0 if not found
		if minCount == -1 || count < minCount {
			minCount = count
			selectedOperator = operator
		}
	}

	return selectedOperator
}

func TestSelectOperatorFromCounts(t *testing.T) {
	operators := []string{
		"operator1@example.com",
		"operator2@example.com",
		"operator3@example.com",
	}

	taskCounts := map[string]int{
		"operator1@example.com": 5,
		"operator2@example.com": 3, // Should be selected (lowest count)
		"operator3@example.com": 7,
	}

	selected := selectOperatorFromCounts(operators, taskCounts)
	expected := "operator2@example.com"

	if selected != expected {
		t.Errorf("selectOperatorFromCounts() = %s, want %s", selected, expected)
	}
}

func TestSelectOperatorFromCounts_EmptyOperators(t *testing.T) {
	var operators []string
	taskCounts := map[string]int{}

	selected := selectOperatorFromCounts(operators, taskCounts)
	if selected != "" {
		t.Errorf("selectOperatorFromCounts() with empty operators should return empty string, got %s", selected)
	}
}

func TestSelectOperatorFromCounts_EqualCounts(t *testing.T) {
	operators := []string{
		"operator1@example.com",
		"operator2@example.com",
		"operator3@example.com",
	}

	taskCounts := map[string]int{
		"operator1@example.com": 2,
		"operator2@example.com": 2,
		"operator3@example.com": 2,
	}

	// Should select first operator when counts are equal
	selected := selectOperatorFromCounts(operators, taskCounts)
	expected := "operator1@example.com"

	if selected != expected {
		t.Errorf("selectOperatorFromCounts() with equal counts = %s, want %s", selected, expected)
	}
}

func TestSelectOperatorFromCounts_MissingCounts(t *testing.T) {
	operators := []string{
		"operator1@example.com",
		"operator2@example.com",
	}

	// Empty task counts - should default to 0 and select first operator
	taskCounts := map[string]int{}

	selected := selectOperatorFromCounts(operators, taskCounts)
	expected := "operator1@example.com"

	if selected != expected {
		t.Errorf("selectOperatorFromCounts() with missing counts = %s, want %s", selected, expected)
	}
}
