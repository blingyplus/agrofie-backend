package payments

// Provider is the application-level payment hold interface.
// Paystack Ghana does not offer true escrow products; Agrofie holds funds
// on the platform merchant account and records them in escrow_ledger, then
// releases via Transfer API. Do not treat Paystack Split as escrow.
type Provider interface {
	Hold(bookingID string, amountMinor int64, currency string) (providerRef string, err error)
	Release(providerRef string) error
	Refund(providerRef string) error
}

// FakeProvider is a no-op adapter for local scaffold / tests.
type FakeProvider struct{}

func (FakeProvider) Hold(bookingID string, amountMinor int64, currency string) (string, error) {
	return "fake-hold-" + bookingID, nil
}

func (FakeProvider) Release(providerRef string) error { return nil }

func (FakeProvider) Refund(providerRef string) error { return nil }
