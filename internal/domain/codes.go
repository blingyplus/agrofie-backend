package domain

// Role codes — must match roles.code seed values. Prefer loading from DB for display names.
const (
	RoleTalent    = "talent"
	RoleOrganizer = "organizer"
	RoleAdmin     = "admin"
)

// BookingStatus codes — must match booking_statuses.code seed values.
const (
	BookingStatusInquiry   = "inquiry"
	BookingStatusAgreed    = "agreed"
	BookingStatusPaid      = "paid"
	BookingStatusCompleted = "completed"
	BookingStatusCancelled = "cancelled"
)

// LedgerEntryType codes — must match ledger_entry_types.code seed values.
const (
	LedgerHold       = "hold"
	LedgerRelease    = "release"
	LedgerRefund     = "refund"
	LedgerCommission = "commission"
)
