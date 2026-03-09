from konvu_cli.mapping import (
    AssessmentStatus,
    assessment_to_recommendation,
    recommendation_to_assessment,
)


class TestRecommendationToAssessment:
    def test_to_fix_maps_to_exploitable(self) -> None:
        assert recommendation_to_assessment("to_fix") == AssessmentStatus.EXPLOITABLE

    def test_to_dismiss_maps_to_false_positive(self) -> None:
        assert (
            recommendation_to_assessment("to_dismiss")
            == AssessmentStatus.FALSE_POSITIVE
        )

    def test_no_qualification_maps_to_not_assessed(self) -> None:
        assert (
            recommendation_to_assessment("no_qualification")
            == AssessmentStatus.NOT_ASSESSED
        )

    def test_monitoring_maps_to_inconclusive(self) -> None:
        assert (
            recommendation_to_assessment("monitoring") == AssessmentStatus.INCONCLUSIVE
        )

    def test_install_runtime_maps_to_inconclusive(self) -> None:
        assert (
            recommendation_to_assessment("install_runtime")
            == AssessmentStatus.INCONCLUSIVE
        )

    def test_none_maps_to_inconclusive(self) -> None:
        assert recommendation_to_assessment(None) == AssessmentStatus.INCONCLUSIVE


class TestAssessmentToRecommendation:
    def test_exploitable_maps_to_to_fix(self) -> None:
        assert assessment_to_recommendation(AssessmentStatus.EXPLOITABLE) == ["to_fix"]

    def test_false_positive_maps_to_to_dismiss(self) -> None:
        assert assessment_to_recommendation(AssessmentStatus.FALSE_POSITIVE) == [
            "to_dismiss"
        ]

    def test_not_assessed_maps_to_no_qualification(self) -> None:
        result = assessment_to_recommendation(AssessmentStatus.NOT_ASSESSED)
        assert "no_qualification" in result
        assert "no_recommendation" in result

    def test_inconclusive_maps_to_multiple(self) -> None:
        result = assessment_to_recommendation(AssessmentStatus.INCONCLUSIVE)
        assert "monitoring" in result
        assert "install_runtime" in result
