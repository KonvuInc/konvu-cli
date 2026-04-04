package mapping

type AssessmentStatus string

const (
	Exploitable   AssessmentStatus = "exploitable"
	FalsePositive AssessmentStatus = "false-positive"
	NeedsInput    AssessmentStatus = "needs-input"
	Inconclusive  AssessmentStatus = "inconclusive"
	NotAssessed   AssessmentStatus = "not-assessed"
)

var AllStatuses = []AssessmentStatus{Exploitable, FalsePositive, NeedsInput, Inconclusive, NotAssessed}

const (
	RecToFix            = "to_fix"
	RecToDismiss        = "to_dismiss"
	RecMonitoring       = "monitoring"
	RecInstallRuntime   = "install_runtime"
	RecInstallGithub    = "install_github"
	RecNoRecommendation = "no_recommendation"
	RecNoQualification  = "no_qualification"
)

func AssessmentToRecommendation(a AssessmentStatus) []string {
	switch a {
	case Exploitable:
		return []string{RecToFix}
	case FalsePositive:
		return []string{RecToDismiss}
	case NotAssessed:
		return []string{RecNoQualification, RecNoRecommendation}
	case NeedsInput, Inconclusive:
		return []string{RecMonitoring, RecInstallRuntime, RecInstallGithub}
	default:
		return []string{RecMonitoring, RecInstallRuntime, RecInstallGithub}
	}
}

var assessmentColors = map[AssessmentStatus]string{
	Exploitable:   "\033[1;31m",
	FalsePositive: "\033[32m",
	NeedsInput:    "\033[34m",
	Inconclusive:  "\033[2m",
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

