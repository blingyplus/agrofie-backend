package payments_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/blingyplus/agrofie-backend/internal/payments"
)

func TestCreateSubaccountSendsCommissionAndAccountDetails(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("/subaccount", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  true,
			"message": "Subaccount created",
			"data": map[string]any{
				"subaccount_code": "ACCT_abc123",
				"account_name":    "Ama Serwaa",
				"settlement_bank": "GCB Bank",
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := payments.NewPaystack("sk_test_secret").WithBaseURL(srv.URL)
	sub, err := p.CreateSubaccount(context.Background(), payments.SubaccountInput{
		BusinessName:   "Ama Serwaa",
		SettlementType: payments.SettlementBank,
		BankCode:       "058",
		AccountNumber:  "0123456047",
	}, 12.5)
	if err != nil {
		t.Fatal(err)
	}
	if sub.SubaccountCode != "ACCT_abc123" || sub.AccountName != "Ama Serwaa" || sub.BankName != "GCB Bank" {
		t.Fatalf("got %+v", sub)
	}
	if gotAuth != "Bearer sk_test_secret" {
		t.Fatalf("Authorization header = %q", gotAuth)
	}
	if gotBody["business_name"] != "Ama Serwaa" || gotBody["settlement_bank"] != "058" ||
		gotBody["account_number"] != "0123456047" || gotBody["percentage_charge"] != 12.5 {
		t.Fatalf("request body = %+v", gotBody)
	}
}

func TestCreateSubaccountReturnsPaystackErrorMessage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/subaccount", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": false, "message": "Invalid account number"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	_, err := payments.NewPaystack("sk_test").WithBaseURL(srv.URL).
		CreateSubaccount(context.Background(), payments.SubaccountInput{AccountNumber: "bad"}, 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "Invalid account number") {
		t.Fatalf("error = %q, want it to mention Paystack's message", got)
	}
}

func TestListSettlementBanksSelectsMobileMoneyType(t *testing.T) {
	var gotQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/bank", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": true,
			"data": []map[string]any{
				{"name": "MTN Mobile Money", "code": "MTN"},
				{"name": "Telecel Cash", "code": "TCL"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	banks, err := payments.NewPaystack("sk_test").WithBaseURL(srv.URL).
		ListSettlementBanks(context.Background(), "GHS", payments.SettlementMobileMoney)
	if err != nil {
		t.Fatal(err)
	}
	if len(banks) != 2 || banks[0].Code != "MTN" {
		t.Fatalf("got %+v", banks)
	}
	if !strings.Contains(gotQuery, "type=mobile_money") || !strings.Contains(gotQuery, "currency=GHS") {
		t.Fatalf("query = %q", gotQuery)
	}
}
