// Package payments talks to Paystack. Agrofie never holds client funds: the
// organizer pays through Paystack Checkout with a split, Paystack settles the
// talent's share to their own subaccount and Agrofie's commission to Agrofie.
// See docs/PRODUCT.md (Money model) and docs/ARCHITECTURE.md.
package payments

import "context"

type SettlementType string

const (
	SettlementBank        SettlementType = "bank"
	SettlementMobileMoney SettlementType = "mobile_money"
)

// SubaccountInput describes the payout target for one talent.
type SubaccountInput struct {
	BusinessName   string
	SettlementType SettlementType
	BankCode       string // Paystack bank code, or telco code for mobile money
	AccountNumber  string
}

// Subaccount is what Paystack returns after verifying the account.
type Subaccount struct {
	SubaccountCode string
	AccountName    string // name on the account, as resolved by the bank/telco
	BankName       string
}

// Bank is one settlement option (a bank or, for Ghana, a mobile money telco).
type Bank struct {
	Name string
	Code string
}

// CheckoutInput describes one split checkout: the organizer pays
// AmountPesewas total, of which CommissionPesewas goes to Agrofie (as a flat
// transaction_charge) and the rest settles to SubaccountCode. The talent
// bears Paystack's own processing fee (bearer: subaccount).
type CheckoutInput struct {
	Reference         string // idempotency key; must be unique per attempt
	PayerEmail        string
	AmountPesewas     int64
	CommissionPesewas int64
	SubaccountCode    string
	CurrencyCode      string
}

// Checkout is what Paystack returns to start a transaction.
type Checkout struct {
	CheckoutURL string
	Reference   string
}

// TransactionStatus is Paystack's own transaction status: "success",
// "failed", "abandoned", etc.
type TransactionStatus string

// Provider is the payment port. It creates and looks up subaccounts for
// split payments and runs Checkout; it never moves money on Agrofie's
// behalf — Paystack settles each party directly.
type Provider interface {
	// ListSettlementBanks lists banks (settlementType "bank") or mobile money
	// telcos (settlementType "mobile_money") for the given currency.
	ListSettlementBanks(ctx context.Context, currency string, settlementType SettlementType) ([]Bank, error)
	// CreateSubaccount creates or updates a talent's payout subaccount.
	// commissionPercent is the percentage Agrofie keeps; the remainder settles
	// to the subaccount. The talent bears Paystack's processing fee.
	CreateSubaccount(ctx context.Context, in SubaccountInput, commissionPercent float64) (Subaccount, error)
	// InitializeCheckout starts a split transaction and returns the URL to
	// send the payer to.
	InitializeCheckout(ctx context.Context, in CheckoutInput) (Checkout, error)
	// VerifyTransaction is the reconciliation fallback for when a webhook
	// hasn't arrived (see docs/ARCHITECTURE.md: 180s timeout).
	VerifyTransaction(ctx context.Context, reference string) (TransactionStatus, error)
	// VerifyWebhookSignature checks the raw request body against Paystack's
	// x-paystack-signature header (HMAC-SHA512). Never trust a webhook body
	// before this passes.
	VerifyWebhookSignature(rawBody []byte, signatureHeader string) bool
}

// FakeProvider is a no-op adapter for local dev and tests. It is used
// automatically when PAYSTACK_SECRET_KEY is not set.
type FakeProvider struct{}

func (FakeProvider) ListSettlementBanks(_ context.Context, _ string, t SettlementType) ([]Bank, error) {
	if t == SettlementMobileMoney {
		return []Bank{{Name: "MTN Mobile Money", Code: "MTN"}, {Name: "Telecel Cash", Code: "TCL"}, {Name: "AirtelTigo Money", Code: "ATL"}}, nil
	}
	return []Bank{{Name: "Fake Test Bank", Code: "999"}}, nil
}

func (FakeProvider) CreateSubaccount(_ context.Context, in SubaccountInput, _ float64) (Subaccount, error) {
	return Subaccount{
		SubaccountCode: "ACCT_fake_" + in.AccountNumber,
		AccountName:    in.BusinessName,
		BankName:       "Fake Test Bank",
	}, nil
}

func (FakeProvider) InitializeCheckout(_ context.Context, in CheckoutInput) (Checkout, error) {
	return Checkout{CheckoutURL: "https://checkout.fake.test/" + in.Reference, Reference: in.Reference}, nil
}

func (FakeProvider) VerifyTransaction(_ context.Context, reference string) (TransactionStatus, error) {
	return "success", nil
}

func (FakeProvider) VerifyWebhookSignature(_ []byte, _ string) bool { return true }

var _ Provider = FakeProvider{}
