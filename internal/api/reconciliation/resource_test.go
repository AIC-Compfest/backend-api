package reconciliation_test

import (
	"testing"

	apireconcile "github.com/AIC-Compfest/backend-api/internal/api/reconciliation"
	"github.com/AIC-Compfest/backend-api/internal/model"
	reconcilesvc "github.com/AIC-Compfest/backend-api/internal/service/reconciliation"
)

func setupTestResource() *apireconcile.Resource {
	engine := reconcilesvc.NewEngine()
	repo := apireconcile.NewMemoryRepository()
	return apireconcile.NewResource(engine, repo)
}

func TestResource_EndToEndWorkflow(t *testing.T) {
	res := setupTestResource()

	req := apireconcile.ReconcileRequestDTO{
		Invoice: model.InvoiceCanonical{
			InvoiceID:        "INV-PHASE12-001",
			InvoiceNumber:    "INV/2026/08/P12",
			InvoiceDate:      "2026-08-10",
			VendorID:         "3PL-LOG-001",
			ServiceType:      "REGULAR",
			Origin:           "SURABAYA",
			Destination:      "JAKARTA",
			WeightBilledKg:   50.0,
			BaseChargeBilled: 750000, // Overcharge
			BilledAmount:     832500,
			TaxBilled:        82500,
			Currency:         "IDR",
		},
		Shipment: model.ShipmentCanonical{
			SuratJalanNumber: "SJ-PHASE12-001",
			ShipmentID:       "SHP-PHASE12-001",
			AWBNumber:        "AWB998811",
			ShipmentDate:     "2026-08-08",
			Origin:           "SURABAYA",
			Destination:      "JAKARTA",
			WeightActualKg:   50.0,
			TotalPackages:    10,
		},
		POD: &model.PODCanonical{
			PODNumber:           "POD-PHASE12-001",
			DeliveryStatus:      "DELIVERED",
			DeliveryDate:        "2026-08-10",
			SignaturePresent:    true,
			SignatureConfidence: 0.95,
			StampPresent:        true,
		},
		Contract: model.ContractCanonical{
			AgreementID:                    "CTR-3PL-LOG-001-2026",
			VendorID:                       "3PL-LOG-001",
			EffectiveFrom:                  "2026-01-01",
			EffectiveTo:                    "2026-12-31",
			BaseRate:                       480000,
			ApplicableFuelSurchargePercent: 14.0,
			DisputeWindowDays:              45,
		},
	}

	// 1. Reconcile
	event, err := res.Reconcile(req)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	if event.Reconciliation.Status != model.StatusException {
		t.Errorf("Expected EXCEPTION for rate overcharge, got %s", event.Reconciliation.Status)
	}

	// 2. Fetch Queue
	qResp, err := res.GetQueue(apireconcile.QueueQueryDTO{})
	if err != nil {
		t.Fatalf("GetQueue error: %v", err)
	}
	if len(qResp.Items) != 1 {
		t.Fatalf("Expected 1 queue item, got %d", len(qResp.Items))
	}
	if qResp.Summary.OpenExceptionsCount != 1 {
		t.Errorf("Expected 1 open exception in summary, got %d", qResp.Summary.OpenExceptionsCount)
	}

	// 3. Get Detail
	detail, err := res.GetEventByID(event.EventID)
	if err != nil {
		t.Fatalf("GetEventByID error: %v", err)
	}
	if detail.ShipmentID != "SHP-PHASE12-001" {
		t.Errorf("Expected shipment ID SHP-PHASE12-001, got %s", detail.ShipmentID)
	}

	// 4. Submit Dispute Decision
	decReq := apireconcile.DecisionRequestDTO{
		Action:       "DISPUTE",
		ReviewerID:   "USER-FIN-01",
		ReviewerRole: "FINANCE_CONTROLLER",
		Reason:       "Base rate charged Rp 750,000 exceeds contracted rate Rp 480,000",
	}
	updatedEv, err := res.SubmitDecision(event.EventID, decReq)
	if err != nil {
		t.Fatalf("SubmitDecision error: %v", err)
	}
	if len(updatedEv.AuditTrail) < 2 {
		t.Fatalf("Expected at least 2 audit trail items, got %d", len(updatedEv.AuditTrail))
	}
	lastAudit := updatedEv.AuditTrail[len(updatedEv.AuditTrail)-1]
	if lastAudit.Action != "DISPUTE" || lastAudit.ActorRole != "FINANCE_CONTROLLER" {
		t.Errorf("Unexpected audit trail record: %+v", lastAudit)
	}

	// 5. Generate Dispute Package
	pkg, err := res.GenerateDisputePackage(event.EventID)
	if err != nil {
		t.Fatalf("GenerateDisputePackage error: %v", err)
	}
	if pkg.DisputeAmount <= 0 {
		t.Errorf("Expected positive dispute amount, got %d", pkg.DisputeAmount)
	}
	if pkg.ReviewerAction != "DISPUTE" {
		t.Errorf("Expected reviewer action DISPUTE in package, got %s", pkg.ReviewerAction)
	}
}

func TestResource_MandatoryReasonValidation(t *testing.T) {
	res := setupTestResource()
	req := apireconcile.ReconcileRequestDTO{
		Invoice: model.InvoiceCanonical{
			InvoiceNumber: "INV-VAL-001",
			InvoiceDate:   "2026-08-10",
			VendorID:      "3PL-LOG-001",
			BilledAmount:  600000,
		},
		Shipment: model.ShipmentCanonical{
			ShipmentID:     "SHP-VAL-001",
			ShipmentDate:   "2026-08-08",
			WeightActualKg: 10.0,
		},
		Contract: model.ContractCanonical{
			AgreementID: "CTR-001",
			VendorID:    "3PL-LOG-001",
			BaseRate:    400000,
		},
	}

	event, err := res.Reconcile(req)
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}

	// Empty reason on DISPUTE must fail
	_, err = res.SubmitDecision(event.EventID, apireconcile.DecisionRequestDTO{
		Action: "DISPUTE",
		Reason: "", // Empty reason
	})
	if err == nil {
		t.Errorf("Expected error for empty reason on DISPUTE action, got nil")
	}

	// Invalid role must fail
	_, err = res.SubmitDecision(event.EventID, apireconcile.DecisionRequestDTO{
		Action:       "DISPUTE",
		Reason:       "Valid reason",
		ReviewerRole: "INVALID_HACKER_ROLE",
	})
	if err == nil {
		t.Errorf("Expected error for invalid reviewer role, got nil")
	}
}
