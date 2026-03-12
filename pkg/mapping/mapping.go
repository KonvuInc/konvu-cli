package mapping

type AssessmentStatus string

const (
	Exploitable   AssessmentStatus = "exploitable"
	FalsePositive AssessmentStatus = "false-positive"
	Inconclusive  AssessmentStatus = "inconclusive"
	NotAssessed   AssessmentStatus = "not-assessed"
)

var AllStatuses = []AssessmentStatus{Exploitable, FalsePositive, Inconclusive, NotAssessed}

const (
	RecToFix            = "to_fix"
	RecToDismiss        = "to_dismiss"
	RecMonitoring       = "monitoring"
	RecInstallRuntime   = "install_runtime"
	RecInstallGithub    = "install_github"
	RecNoRecommendation = "no_recommendation"
	RecNoQualification  = "no_qualification"
)

func RecommendationToAssessment(rec string) AssessmentStatus {
	switch rec {
	case RecToFix:
		return Exploitable
	case RecToDismiss:
		return FalsePositive
	case RecNoQualification:
		return NotAssessed
	default:
		return Inconclusive
	}
}

func AssessmentToRecommendation(a AssessmentStatus) []string {
	switch a {
	case Exploitable:
		return []string{RecToFix}
	case FalsePositive:
		return []string{RecToDismiss}
	case NotAssessed:
		return []string{RecNoQualification, RecNoRecommendation}
	default:
		return []string{RecMonitoring, RecInstallRuntime, RecInstallGithub}
	}
}

var assessmentColors = map[AssessmentStatus]string{
	Exploitable:   "\033[1;31m",
	FalsePositive: "\033[32m",
	Inconclusive:  "\033[33m",
	NotAssessed:   "\033[2m",
}

const colorReset = "\033[0m"

func GetAssessmentColor(status string) string {
	return assessmentColors[AssessmentStatus(status)]
}

func ColorReset() string {
	return colorReset
}

func Colorize(text string, status AssessmentStatus) string {
	c := assessmentColors[status]
	if c == "" {
		return text
	}
	return c + text + colorReset
}

func GetAssessmentSummary(a AssessmentStatus) (summary, nextSteps string) {
	switch a {
	case Exploitable:
		return "A vulnerable function is being executed in your application.",
			"Prioritize remediation of this vulnerability."
	case FalsePositive:
		return "Not exploitable in your context.",
			"You may deprioritize remediation of this vulnerability."
	case NotAssessed:
		return "This vulnerability has not been assessed yet.",
			"Additional analysis may be required."
	default:
		return "Unable to determine exploitability with high confidence.",
			"Review the exploitability conditions manually."
	}
}
