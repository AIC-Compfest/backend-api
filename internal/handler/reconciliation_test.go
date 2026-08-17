package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AIC-Compfest/backend-api/internal/app"
	"github.com/AIC-Compfest/backend-api/internal/config"
	"github.com/AIC-Compfest/backend-api/internal/handler"
	"github.com/AIC-Compfest/backend-api/internal/model"
	"github.com/AIC-Compfest/backend-api/internal/service/reconciliation"
)

func TestReconciliationEndpoints(t *testing.T) {
	cfg := config.Config{Port: "8080"}
	application := app.New(cfg)
	router := application.Router()

	reqPayload := handler.ReconcileRequest{
		Invoice: model.InvoiceCanonical{
			InvoiceID:        "INV-TEST-001",
			InvoiceNumber:    "INV/2026/08/0001",
			InvoiceDate:      "2026-08-10",
			VendorID:         "3PL-LOG-001",
			ServiceType:      "REGULAR",
			Origin:           "SURABAYA",
			Destination:      "JAKARTA",
			WeightBilledKg:   84.33,
			BaseChargeBilled: 480000,
			BilledAmount:     607392,
			TaxBilled:        60192,
			Currency:         "IDR",
		},
		Shipment: model.ShipmentCanonical{
			SuratJalanNumber: "SJ-2026-0001",
			ShipmentID:       "SHP-2026-0001",
			ShipmentDate:     "2026-08-08",
			Origin:           "SURABAYA",
			Destination:      "JAKARTA",
			WeightActualKg:   84.33,
			TotalPackages:    16,
		},
		POD: &model.PODCanonical{
			PODNumber:           "POD-2026-0001",
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

	body, _ := json.Marshal(reqPayload)

	// 1. Test POST /api/v1/reconcile
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/reconcile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// 2. Test GET /api/v1/reconcile/queue
	wQ := httptest.NewRecorder()
	reqQ, _ := http.NewRequest(http.MethodGet, "/api/v1/reconcile/queue", nil)
	router.ServeHTTP(wQ, reqQ)

	if wQ.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /queue, got %d: %s", wQ.Code, wQ.Body.String())
	}

	var qResp struct {
		Items   []reconciliation.QueueItem   `json:"items"`
		Summary reconciliation.QueueSummary `json:"summary"`
	}
	if err := json.Unmarshal(wQ.Body.Bytes(), &qResp); err != nil {
		t.Fatalf("failed to decode queue response: %v", err)
	}

	if len(qResp.Items) == 0 {
		t.Errorf("expected at least 1 item in seeded queue")
	}

	firstEventID := qResp.Items[0].EventID

	// 3. Test GET /api/v1/reconcile/:id
	wGet := httptest.NewRecorder()
	reqGet, _ := http.NewRequest(http.MethodGet, "/api/v1/reconcile/"+firstEventID, nil)
	router.ServeHTTP(wGet, reqGet)

	if wGet.Code != http.StatusOK {
		t.Fatalf("expected status 200 on GET /:id, got %d: %s", wGet.Code, wGet.Body.String())
	}

	// 4. Test POST /api/v1/reconcile/:id/decision (DISPUTE)
	decReq := reconciliation.DecisionRequest{
		Action:       "DISPUTE",
		ReviewerID:   "USER-AP-001",
		ReviewerRole: "AP_MANAGER",
		Reason:       "Base rate exceeds contracted tariff schedule",
	}
	decBody, _ := json.Marshal(decReq)

	wDec := httptest.NewRecorder()
	reqDec, _ := http.NewRequest(http.MethodPost, "/api/v1/reconcile/"+firstEventID+"/decision", bytes.NewReader(decBody))
	reqDec.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(wDec, reqDec)

	if wDec.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /decision, got %d: %s", wDec.Code, wDec.Body.String())
	}

	// 5. Test GET /api/v1/reconcile/:id/dispute-package
	wDisp := httptest.NewRecorder()
	reqDisp, _ := http.NewRequest(http.MethodGet, "/api/v1/reconcile/"+firstEventID+"/dispute-package", nil)
	router.ServeHTTP(wDisp, reqDisp)

	if wDisp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /dispute-package, got %d: %s", wDisp.Code, wDisp.Body.String())
	}

	var pkg reconciliation.DisputePackage
	if err := json.Unmarshal(wDisp.Body.Bytes(), &pkg); err != nil {
		t.Fatalf("failed to decode dispute package: %v", err)
	}
	if pkg.FormattedMemo == "" {
		t.Errorf("expected non-empty formatted memo in dispute package")
	}

	// 6. Security Adversarial Tests: Missing Reason on DISPUTE
	emptyReasonReq := reconciliation.DecisionRequest{
		Action:       "DISPUTE",
		ReviewerID:   "USER-AP-001",
		ReviewerRole: "AP_MANAGER",
		Reason:       "   ", // Empty whitespace reason
	}
	emptyBody, _ := json.Marshal(emptyReasonReq)
	wEmpty := httptest.NewRecorder()
	reqEmpty, _ := http.NewRequest(http.MethodPost, "/api/v1/reconcile/"+firstEventID+"/decision", bytes.NewReader(emptyBody))
	reqEmpty.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(wEmpty, reqEmpty)
	if wEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on empty dispute reason, got %d", wEmpty.Code)
	}

	// 7. Security Adversarial Tests: Invalid Role
	invalidRoleReq := reconciliation.DecisionRequest{
		Action:       "APPROVE",
		ReviewerID:   "USER-AP-001",
		ReviewerRole: "UNAUTHORIZED_ACTOR",
		Reason:       "Testing unauthorized role",
	}
	invalidRoleBody, _ := json.Marshal(invalidRoleReq)
	wRole := httptest.NewRecorder()
	reqRole, _ := http.NewRequest(http.MethodPost, "/api/v1/reconcile/"+firstEventID+"/decision", bytes.NewReader(invalidRoleBody))
	reqRole.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(wRole, reqRole)
	if wRole.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on invalid role, got %d", wRole.Code)
	}

	// 8. Security Adversarial Tests: Invalid Action
	invalidActReq := reconciliation.DecisionRequest{
		Action:       "EXECUTE_PAYMENT",
		ReviewerID:   "USER-AP-001",
		ReviewerRole: "AP_MANAGER",
	}
	invalidActBody, _ := json.Marshal(invalidActReq)
	wAct := httptest.NewRecorder()
	reqAct, _ := http.NewRequest(http.MethodPost, "/api/v1/reconcile/"+firstEventID+"/decision", bytes.NewReader(invalidActBody))
	reqAct.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(wAct, reqAct)
	if wAct.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request on invalid action, got %d", wAct.Code)
	}
}
