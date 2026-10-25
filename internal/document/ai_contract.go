package document

import "fmt"

// pageEngineUsed maps ai-service page metadata without inventing an engine when
// the old AI response did not include it.
func pageEngineUsed(result *AIExtractionResult, page AIPageResult) string {
	if page.EngineUsed != "" {
		if page.FallbackUsed {
			return page.EngineUsed + ":fallback"
		}
		return page.EngineUsed
	}
	if result != nil && result.PipelineVersion != "" {
		return "ai-service:" + result.PipelineVersion
	}
	return "ai-service"
}

// validateAIResult enforces only the transport contract. Business decisions
// remain in backend-api; REQUIRES_HUMAN_REVIEW is a valid AI outcome.
func validateAIResult(result *AIExtractionResult) error {
	if result == nil {
		return fmt.Errorf("ai-service returned an empty response")
	}
	if result.Status == "" {
		return fmt.Errorf("ai-service response is missing status")
	}
	if result.Status == "SUCCESS" && result.DocumentID == "" {
		return fmt.Errorf("ai-service success response is missing document_id")
	}
	return nil
}
