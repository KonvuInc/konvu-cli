package mapping

import "testing"

func TestRecommendationToAssessment(t *testing.T) {
	tests := []struct {
		rec  string
		want AssessmentStatus
	}{
		{"to_fix", Exploitable},
		{"to_dismiss", FalsePositive},
		{"no_qualification", NotAssessed},
		{"monitoring", Inconclusive},
		{"install_runtime", Inconclusive},
		{"install_github", Inconclusive},
		{"no_recommendation", Inconclusive},
		{"", Inconclusive},
	}
	for _, tt := range tests {
		got := RecommendationToAssessment(tt.rec)
		if got != tt.want {
			t.Errorf("RecommendationToAssessment(%q) = %q, want %q", tt.rec, got, tt.want)
		}
	}
}

func TestAssessmentToRecommendation(t *testing.T) {
	recs := AssessmentToRecommendation(Exploitable)
	if len(recs) != 1 || recs[0] != "to_fix" {
		t.Errorf("AssessmentToRecommendation(Exploitable) = %v, want [to_fix]", recs)
	}

	recs = AssessmentToRecommendation(NotAssessed)
	if len(recs) != 2 {
		t.Errorf("AssessmentToRecommendation(NotAssessed) = %v, want 2 items", recs)
	}
}

func TestGetAssessmentColor(t *testing.T) {
	if c := GetAssessmentColor(string(Exploitable)); c == "" {
		t.Error("GetAssessmentColor(exploitable) returned empty string")
	}
}

func TestGetAssessmentSummary(t *testing.T) {
	summary, nextSteps := GetAssessmentSummary(Exploitable)
	if summary == "" || nextSteps == "" {
		t.Error("GetAssessmentSummary(Exploitable) returned empty strings")
	}
}
