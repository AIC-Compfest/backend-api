package reconciliation

import (
	"time"

	"backend-api/internal/model"
)

// QueueItemDTO represents a prioritized transaction item in the reviewer queue
type QueueItemDTO struct {
	EventID        string  `json:"event_id"`
	TransactionID  string  `json:"transaction_id"`
	ShipmentID     string  `json:"shipment_id"`
	InvoiceNumber  string  `json:"invoice_number"`
	VendorID       string  `json:"vendor_id"`
	VendorName     string  `json:"vendor_name"`
	BilledAmount   float64 `json:"billed_amount"`
	ExpectedAmount float64 `json:"expected_amount"`
	VarianceAmount float64 `json:"variance_amount"`
	MatchScore     float64 `json:"match_score"`
	PriorityScore  float64 `json:"priority_score"`
	Status             string  `json:"status"` // MATCH, EXCEPTION, REVIEWED, APPROVED, DISPUTED, REJECTED
	Severity           string  `json:"severity"`
	PrimaryDiscrepancy string  `json:"primary_discrepancy,omitempty"`
	TopDiscrepancy     string  `json:"top_discrepancy,omitempty"`
	CreatedAt          string  `json:"created_at"`
}

// QueueSummaryDTO holds aggregated metrics for the overview dashboard
type QueueSummaryDTO struct {
	TotalInvoicedAmount float64 `json:"total_invoiced_amount"`
	TotalVarianceAmount float64 `json:"total_variance_amount"`
	TotalInvoicesCount  int     `json:"total_invoices_count"`
	OpenExceptionsCount int     `json:"open_exceptions_count"`
	UrgentDisputesCount int     `json:"urgent_disputes_count"`
	MatchesCount        int     `json:"matches_count"`
	TotalItems          int     `json:"total_items"`
	CleanMatchesCount   int     `json:"clean_matches_count"`
	ResolvedCount       int     `json:"resolved_count"`
	TotalBilledIDR      float64 `json:"total_billed_idr"`
	TotalVarianceIDR    float64 `json:"total_variance_idr"`
	LeakagePreventedIDR float64 `json:"leakage_prevented_idr"`
}

// QueueResponseDTO holds paginated queue items and summary
type QueueResponseDTO struct {
	Summary    QueueSummaryDTO `json:"summary"`
	Items      []QueueItemDTO  `json:"items"`
	Total      int64           `json:"total"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	TotalPages int             `json:"total_pages"`
}

// DecisionRequest payload
type DecisionRequest struct {
	Decision     string   `json:"decision"` // APPROVE, REJECT, DISPUTE, OVERRIDE
	Action       string   `json:"action"`   // Alias for decision
	Reason       string   `json:"reason"`
	ReviewerID   string   `json:"reviewer_id"`
	Role         string   `json:"role"`
	ReviewerRole string   `json:"reviewer_role"` // Alias for role
	Tags         []string `json:"tags,omitempty"`
}

// DecisionResponse payload
type DecisionResponse struct {
	EventID        string               `json:"event_id"`
	PreviousStatus string               `json:"previous_status"`
	NewStatus      string               `json:"new_status"`
	Decision       model.AuditTrailItem `json:"decision"`
	AuditLogID     string               `json:"audit_log_id"`
	Timestamp      time.Time            `json:"timestamp"`
}

// DisputePackageDTO contains official claim package details
type DisputePackageDTO struct {
	EventID          string                  `json:"event_id"`
	TransactionID    string                  `json:"transaction_id"`
	VendorName       string                  `json:"vendor_name"`
	VendorID         string                  `json:"vendor_id"`
	InvoiceNumber    string                  `json:"invoice_number"`
	ShipmentID       string                  `json:"shipment_id"`
	BilledAmount     float64                 `json:"billed_amount"`
	ExpectedAmount   float64                 `json:"expected_amount"`
	DisputedAmount   float64                 `json:"disputed_amount"`
	GeneratedAt      time.Time               `json:"generated_at"`
	GeneratedBy      string                  `json:"generated_by"`
	DiscrepancyItems []model.DiscrepancyItem `json:"discrepancy_items"`
	FormattedMemo    string                  `json:"formatted_memo"`
}

// QueueFilter holds query filters
type QueueFilter struct {
	Page     int
	PageSize int
	Status   string
	Severity string
	Vendor   string
	Search   string
}
