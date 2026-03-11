"""Maps between backend recommendation and customer-facing assessment terminology.

This matches the logic in dashboard/src/components/TriageStatusBadge.tsx
"""

from enum import Enum


class AssessmentStatus(str, Enum):
    """Customer-facing assessment status."""

    EXPLOITABLE = "exploitable"
    FALSE_POSITIVE = "false-positive"
    INCONCLUSIVE = "inconclusive"
    NOT_ASSESSED = "not-assessed"


# Backend recommendation values
RECOMMENDATION_TO_FIX = "to_fix"
RECOMMENDATION_TO_DISMISS = "to_dismiss"
RECOMMENDATION_MONITORING = "monitoring"
RECOMMENDATION_INSTALL_RUNTIME = "install_runtime"
RECOMMENDATION_INSTALL_GITHUB = "install_github"
RECOMMENDATION_NO_RECOMMENDATION = "no_recommendation"
RECOMMENDATION_NO_QUALIFICATION = "no_qualification"


def recommendation_to_assessment(recommendation: str | None) -> AssessmentStatus:
    """Map backend recommendation to customer-facing assessment status.

    Matches logic from TriageStatusBadge.tsx:recommendationToTriageStatus()
    """
    if recommendation == RECOMMENDATION_TO_FIX:
        return AssessmentStatus.EXPLOITABLE
    if recommendation == RECOMMENDATION_TO_DISMISS:
        return AssessmentStatus.FALSE_POSITIVE
    if recommendation == RECOMMENDATION_NO_QUALIFICATION:
        return AssessmentStatus.NOT_ASSESSED
    # monitoring, install_runtime, install_github, no_recommendation, None
    return AssessmentStatus.INCONCLUSIVE


def assessment_to_recommendation(assessment: AssessmentStatus) -> list[str]:
    """Map customer-facing assessment to backend recommendation values.

    Returns a list because some assessments map to multiple recommendations.
    Used for filtering API queries.
    """
    if assessment == AssessmentStatus.EXPLOITABLE:
        return [RECOMMENDATION_TO_FIX]
    if assessment == AssessmentStatus.FALSE_POSITIVE:
        return [RECOMMENDATION_TO_DISMISS]
    if assessment == AssessmentStatus.NOT_ASSESSED:
        return [RECOMMENDATION_NO_QUALIFICATION, RECOMMENDATION_NO_RECOMMENDATION]
    # INCONCLUSIVE
    return [
        RECOMMENDATION_MONITORING,
        RECOMMENDATION_INSTALL_RUNTIME,
        RECOMMENDATION_INSTALL_GITHUB,
    ]


ASSESSMENT_COLORS: dict[str, str] = {
    AssessmentStatus.EXPLOITABLE.value: "red bold",
    AssessmentStatus.FALSE_POSITIVE.value: "green",
    AssessmentStatus.INCONCLUSIVE.value: "yellow",
    AssessmentStatus.NOT_ASSESSED.value: "dim",
}


def get_assessment_color(status: str) -> str:
    """Return Rich style string for an assessment status."""
    return ASSESSMENT_COLORS.get(status, "")


def get_assessment_summary(assessment: AssessmentStatus) -> tuple[str, str]:
    """Get customer-facing summary and next_steps for an assessment status."""
    if assessment == AssessmentStatus.EXPLOITABLE:
        return (
            "A vulnerable function is being executed in your application.",
            "Prioritize remediation of this vulnerability.",
        )
    if assessment == AssessmentStatus.FALSE_POSITIVE:
        return (
            "Not exploitable in your context.",
            "You may deprioritize remediation of this vulnerability.",
        )
    if assessment == AssessmentStatus.NOT_ASSESSED:
        return (
            "This vulnerability has not been assessed yet.",
            "Additional analysis may be required.",
        )
    # INCONCLUSIVE
    return (
        "Unable to determine exploitability with high confidence.",
        "Review the exploitability conditions manually.",
    )
