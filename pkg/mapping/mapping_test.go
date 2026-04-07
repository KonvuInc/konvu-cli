package mapping

import "testing"

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

