package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Paystack is the real adapter, backed by the Paystack API. baseURL is
// overridable in tests; production code should leave it at the default.
type Paystack struct {
	secretKey string
	baseURL   string
	client    *http.Client
}

func NewPaystack(secretKey string) *Paystack {
	return &Paystack{
		secretKey: secretKey,
		baseURL:   "https://api.paystack.co",
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

// WithBaseURL points the client at a test server. Test-only.
func (p *Paystack) WithBaseURL(u string) *Paystack {
	p.baseURL = strings.TrimRight(u, "/")
	return p
}

type paystackEnvelope[T any] struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

func (p *Paystack) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.secretKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("paystack request: %w", err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("paystack read response: %w", err)
	}
	if res.StatusCode >= 300 {
		var e paystackEnvelope[map[string]any]
		_ = json.Unmarshal(raw, &e)
		msg := e.Message
		if msg == "" {
			msg = string(raw)
		}
		return fmt.Errorf("paystack %s %s: %d %s", method, path, res.StatusCode, msg)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("paystack decode response: %w", err)
	}
	return nil
}

type bankDTO struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

func (p *Paystack) ListSettlementBanks(ctx context.Context, currency string, t SettlementType) ([]Bank, error) {
	kind := "ghipss" // bank
	if t == SettlementMobileMoney {
		kind = "mobile_money"
	}
	q := url.Values{"currency": {currency}, "type": {kind}}
	var res paystackEnvelope[[]bankDTO]
	if err := p.do(ctx, http.MethodGet, "/bank?"+q.Encode(), nil, &res); err != nil {
		return nil, err
	}
	out := make([]Bank, 0, len(res.Data))
	for _, b := range res.Data {
		out = append(out, Bank{Name: b.Name, Code: b.Code})
	}
	return out, nil
}

type createSubaccountBody struct {
	BusinessName     string  `json:"business_name"`
	SettlementBank   string  `json:"settlement_bank"`
	AccountNumber    string  `json:"account_number"`
	PercentageCharge float64 `json:"percentage_charge"`
}

type subaccountDTO struct {
	SubaccountCode string `json:"subaccount_code"`
	AccountName    string `json:"account_name"`
	SettlementBank string `json:"settlement_bank"`
}

func (p *Paystack) CreateSubaccount(ctx context.Context, in SubaccountInput, commissionPercent float64) (Subaccount, error) {
	var res paystackEnvelope[subaccountDTO]
	err := p.do(ctx, http.MethodPost, "/subaccount", createSubaccountBody{
		BusinessName:     in.BusinessName,
		SettlementBank:   in.BankCode,
		AccountNumber:    in.AccountNumber,
		PercentageCharge: commissionPercent,
	}, &res)
	if err != nil {
		return Subaccount{}, err
	}
	return Subaccount{
		SubaccountCode: res.Data.SubaccountCode,
		AccountName:    res.Data.AccountName,
		BankName:       res.Data.SettlementBank,
	}, nil
}

type initializeBody struct {
	Email             string `json:"email"`
	Amount            int64  `json:"amount"`
	Reference         string `json:"reference"`
	Currency          string `json:"currency,omitempty"`
	Subaccount        string `json:"subaccount,omitempty"`
	TransactionCharge int64  `json:"transaction_charge,omitempty"`
	Bearer            string `json:"bearer,omitempty"`
}

type initializeDTO struct {
	AuthorizationURL string `json:"authorization_url"`
	Reference        string `json:"reference"`
}

func (p *Paystack) InitializeCheckout(ctx context.Context, in CheckoutInput) (Checkout, error) {
	var res paystackEnvelope[initializeDTO]
	err := p.do(ctx, http.MethodPost, "/transaction/initialize", initializeBody{
		Email:             in.PayerEmail,
		Amount:            in.AmountPesewas,
		Reference:         in.Reference,
		Currency:          in.CurrencyCode,
		Subaccount:        in.SubaccountCode,
		TransactionCharge: in.CommissionPesewas,
		Bearer:            "subaccount",
	}, &res)
	if err != nil {
		return Checkout{}, err
	}
	return Checkout{CheckoutURL: res.Data.AuthorizationURL, Reference: res.Data.Reference}, nil
}

type verifyDTO struct {
	Status string `json:"status"`
}

func (p *Paystack) VerifyTransaction(ctx context.Context, reference string) (TransactionStatus, error) {
	var res paystackEnvelope[verifyDTO]
	if err := p.do(ctx, http.MethodGet, "/transaction/verify/"+url.PathEscape(reference), nil, &res); err != nil {
		return "", err
	}
	return TransactionStatus(res.Data.Status), nil
}

// VerifyWebhookSignature checks the x-paystack-signature header: it must be
// the hex-encoded HMAC-SHA512 of the raw body, keyed with the secret key.
// Constant-time compare so this can't be timed to leak the signature.
func (p *Paystack) VerifyWebhookSignature(rawBody []byte, signatureHeader string) bool {
	if signatureHeader == "" {
		return false
	}
	mac := hmac.New(sha512.New, []byte(p.secretKey))
	mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signatureHeader)) == 1
}

var _ Provider = (*Paystack)(nil)
