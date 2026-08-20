package reconciliation

import (
	"time"

	"backend-api/internal/model"
)

// ReconcileRequestDTO represents the incoming payload for POST /api/v1/reconcile
type ReconcileRequestDTO struct {
	Invoice  model.InvoiceCanonical  `json:"invoice" binding:"required"`
	Shipment model.ShipmentCanonical `json:"shipment" binding:"required"`
	POD      *model.PODCanonical     `json:"pod,omitempty"`
	Contract model.ContractCanonical `json:"contract" binding:"required"`
}

// DecisionRequestDTO represents the payload for reviewer consequential actions
type DecisionRequestDTO struct {
	Action       string `json:"action" binding:"required"` // APPROVE, DISPUTE, REJECT
	ReviewerID   string `json:"reviewer_id"`
	ReviewerRole string `json:"reviewer_role"` // AP_MANAGER, FINANCE_CONTROLLER, LOGISTICS_MANAGER
	Reason       string `json:"reason"`
}

// QueueQueryDTO represents query parameters for the review queue
type QueueQueryDTO struct {
	StatusFilter   string `form:"status"`
	SeverityFilter string `form:"severity"`
	VendorFilter   string `form:"vendor"`
	Search         string `form:"search"`
}

// QueueItemDTO represents a compact row in the Prioritized Review Queue (PRD §38)
type QueueItemDTO struct {
	EventID                string                     `json:"event_id"`
	TransactionID          string                     `json:"transaction_id"`
	InvoiceNumber          string                     `json:"invoice_number"`
	VendorID               string                     `json:"vendor_id"`
	VendorName             string                     `json:"vendor_name"`
	ShipmentID             string                     `json:"shipment_id"`
	AWBNumber              string                     `json:"awb_number"`
	Status                 model.ReconciliationStatus `json:"status"`
	PrimaryDiscrepancy     string                     `json:"primary_discrepancy,omitempty"`
	DiscrepancyCount       int                        `json:"discrepancy_count"`
	BilledAmount           int64                      `json:"billed_amount"`
	ExpectedAmount         int64                      `json:"expected_amount"`
	VarianceAmount         int64                      `json:"variance_amount"`
	PriorityScore          float64                    `json:"priority_score"`
	PriorityLevel          string                     `json:"priority_level"` // HIGH, MEDIUM, LOW
	DisputeDeadline        string                     `json:"dispute_deadline"`
	DaysRemainingToDispute int                        `json:"days_remaining_to_dispute"`
	IsUrgent               bool                       `json:"is_urgent"` // days_remaining <= 7
}

// QueueSummaryDTO holds high-level KPI strip metrics
type QueueSummaryDTO struct {
	TotalInvoicedAmount int64 `json:"total_invoiced_amount"`
	TotalVarianceAmount int64 `json:"total_variance_amount"`
	TotalInvoicesCount  int   `json:"total_invoices_count"`
	OpenExceptionsCount int   `json:"open_exceptions_count"`
	UrgentDisputesCount int   `json:"urgent_disputes_count"`
	MatchesCount        int   `json:"matches_count"`
}

// QueueResponseDTO holds the full queue response
type QueueResponseDTO struct {
	Items   []QueueItemDTO  `json:"items"`
	Summary QueueSummaryDTO `json:"summary"`
}

// DisputePackageDTO represents the formal downloadable claim memo package (PRD §39)
type DisputePackageDTO struct {
	PackageID       string                  `json:"package_id"`
	GeneratedAt     time.Time               `json:"generated_at"`
	InvoiceNumber   string                  `json:"invoice_number"`
	VendorID        string                  `json:"vendor_id"`
	VendorName      string                  `json:"vendor_name"`
	ShipmentID      string                  `json:"shipment_id"`
	AWBNumber       string                  `json:"awb_number"`
	BilledAmount    int64                   `json:"billed_amount"`
	ExpectedAmount  int64                   `json:"expected_amount"`
	DisputeAmount   int64                   `json:"dispute_amount"`
	Discrepancies   []model.DiscrepancyItem `json:"discrepancies"`
	ContractClause  string                  `json:"contract_clause"`
	Calculation     model.CalculationTrace  `json:"calculation_trace"`
	DaysRemaining   int                     `json:"days_remaining"`
	DisputeDeadline string                  `json:"dispute_deadline"`
	ReviewerAction  string                  `json:"reviewer_action"`
	ReviewerRole    string                  `json:"reviewer_role"`
	ReviewerReason  string                  `json:"reviewer_reason"`
	FormattedMemo   string                  `json:"formatted_memo"`
}

// ErrorResponseDTO represents standard REST error output
type ErrorResponseDTO struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}
