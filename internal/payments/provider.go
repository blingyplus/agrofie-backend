package payments

// Provider is the application-level payment hold port.
// Paystack Ghana does not offer true escrow; Agrofie charges the platform
// merchant, records escrow_ledger rows, then Transfer-releases to talent.
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

// Ensure FakeProvider satisfies Provider at compile time.
var _ Provider = FakeProvider{}
