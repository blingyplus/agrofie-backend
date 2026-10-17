package graph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	agrofierv1 "github.com/blingyplus/agrofie-backend/gen/agrofie/v1"
	"github.com/blingyplus/agrofie-backend/graph/gqlauth"
	"github.com/blingyplus/agrofie-backend/graph/model"
	"github.com/blingyplus/agrofie-backend/internal/availability"
	"github.com/blingyplus/agrofie-backend/internal/booking"
	"github.com/blingyplus/agrofie-backend/internal/checkout"
	"github.com/blingyplus/agrofie-backend/internal/discovery"
	"github.com/blingyplus/agrofie-backend/internal/dispute"
	"github.com/blingyplus/agrofie-backend/internal/lookup"
	"github.com/blingyplus/agrofie-backend/internal/organizerprofile"
	"github.com/blingyplus/agrofie-backend/internal/payments"
	"github.com/blingyplus/agrofie-backend/internal/review"
	"github.com/blingyplus/agrofie-backend/internal/talentprofile"
	"github.com/blingyplus/agrofie-backend/internal/talentrequest"
	"github.com/blingyplus/agrofie-backend/internal/verification"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// Resolver helpers live here, not in schema.resolvers.go: gqlgen owns that file
// and comments out anything it does not recognise when regenerating.

func boolOr(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

func toLookup(item *lookup.Item) *model.Lookup {
	return &model.Lookup{
		ID:        item.ID,
		Code:      item.Code,
		Name:      item.Name,
		SortOrder: int(item.SortOrder),
		IsActive:  item.IsActive,
	}
}

func listLookups(ctx context.Context, activeOnly bool, fn func(context.Context, bool) ([]lookup.Item, error)) ([]*model.Lookup, error) {
	rows, err := fn(ctx, activeOnly)
	if err != nil {
		return nil, mapLookupErr(err)
	}
	out := make([]*model.Lookup, 0, len(rows))
	for i := range rows {
		out = append(out, toLookup(&rows[i]))
	}
	return out, nil
}

func mapLookupErr(err error) error {
	switch {
	case errors.Is(err, lookup.ErrInvalidInput):
		return &gqlerror.Error{Message: err.Error(), Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	case errors.Is(err, lookup.ErrNotFound):
		return &gqlerror.Error{Message: err.Error(), Extensions: map[string]any{"code": "NOT_FOUND"}}
	default:
		return err
	}
}

func toAuthPayload(session *agrofierv1.AuthSession) *model.AuthPayload {
	if session == nil {
		return nil
	}
	return &model.AuthPayload{
		SessionToken: session.GetSessionToken(),
		User:         toModelUser(session.GetUser()),
	}
}

func toModelUser(u *agrofierv1.User) *model.User {
	if u == nil {
		return nil
	}
	var phone *string
	if u.GetPhone() != "" {
		p := u.GetPhone()
		phone = &p
	}
	return &model.User{
		ID:          u.GetId(),
		Email:       u.GetEmail(),
		Phone:       phone,
		DisplayName: u.GetDisplayName(),
		RoleCodes:   append([]string(nil), u.GetRoleCodes()...),
	}
}

func mapConnectErr(err error) error {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return fmt.Errorf("%s", connectErr.Message())
	}
	return err
}

func intOr(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}

func stringOr(v *string, fallback string) string {
	if v == nil {
		return fallback
	}
	return *v
}

func toDiscoveryFilter(f *model.TalentFilter) discovery.Filter {
	if f == nil {
		return discovery.Filter{}
	}
	return discovery.Filter{
		GenreCodes:     f.GenreCodes,
		TypeCodes:      f.TypeCodes,
		LanguageCodes:  f.LanguageCodes,
		PlaceCode:      stringOr(f.PlaceCode, ""),
		FeaturedOnly:   f.FeaturedOnly != nil && *f.FeaturedOnly,
		MinReviewCount: intOr(f.MinReviewCount, 0),
	}
}

func mapDiscoveryErr(err error) error {
	if errors.Is(err, discovery.ErrInvalidCursor) {
		return &gqlerror.Error{Message: "invalid cursor", Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	}
	return err
}

func toTags(in []discovery.Tag) []*model.Tag {
	out := make([]*model.Tag, 0, len(in))
	for _, t := range in {
		out = append(out, &model.Tag{Code: t.Code, Name: t.Name})
	}
	return out
}

func toRate(r discovery.Rate) *model.Rate {
	return &model.Rate{
		Amount:        r.Amount,
		CurrencyCode:  r.CurrencyCode,
		RateUnitCode:  r.RateUnitCode,
		EventTypeCode: r.EventTypeCode,
	}
}

func toTalentCard(c discovery.Card) *model.TalentCard {
	card := &model.TalentCard{
		ID:            c.ID,
		DisplayName:   c.DisplayName,
		Headline:      c.Headline,
		HomePlaceName: c.HomePlaceName,
		Genres:        toTags(c.Genres),
		Types:         toTags(c.Types),
		Featured:      c.Featured,
		RatingAverage: c.RatingAverage,
		ReviewCount:   c.ReviewCount,
		CreatedAt:     c.CreatedAt.UTC().Format(time.RFC3339),
	}
	if c.FromRate != nil {
		card.FromRate = toRate(*c.FromRate)
	}
	return card
}

func toTalentPage(p discovery.Page) *model.TalentPage {
	items := make([]*model.TalentCard, 0, len(p.Items))
	for _, c := range p.Items {
		items = append(items, toTalentCard(c))
	}
	page := &model.TalentPage{Items: items}
	if p.NextCursor != "" {
		next := p.NextCursor
		page.NextCursor = &next
	}
	return page
}

func toTalentProfile(p *discovery.Profile) *model.TalentProfile {
	rates := make([]*model.Rate, 0, len(p.Rates))
	for _, r := range p.Rates {
		rates = append(rates, toRate(r))
	}
	return &model.TalentProfile{
		ID:            p.ID,
		DisplayName:   p.DisplayName,
		Headline:      p.Headline,
		HomePlaceName: p.HomePlaceName,
		Bio:           p.Bio,
		Featured:      p.Featured,
		RatingAverage: p.RatingAverage,
		ReviewCount:   p.ReviewCount,
		CreatedAt:     p.CreatedAt.UTC().Format(time.RFC3339),
		Genres:        toTags(p.Genres),
		Types:         toTags(p.Types),
		Languages:     toTags(p.Languages),
		ServiceAreas:  toTags(p.ServiceAreas),
		Rates:         rates,
	}
}

// ownProfile is shared by myTalentProfile, updateMyTalentProfile and
// setMyRates: fetch the caller's profile fresh after any write, so what a
// mutation returns always matches what a follow-up query would see.
func (r *Resolver) ownProfile(ctx context.Context, userID string) (*model.TalentProfile, error) {
	p, err := r.TalentProfiles.OwnProfile(ctx, userID)
	if err != nil {
		return nil, mapTalentProfileErr(err)
	}
	return toTalentProfile(p), nil
}

func mapTalentProfileErr(err error) error {
	switch {
	case errors.Is(err, talentprofile.ErrUnknownCode):
		return &gqlerror.Error{Message: err.Error(), Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	case errors.Is(err, talentprofile.ErrInvalidInput):
		return &gqlerror.Error{Message: err.Error(), Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	case errors.Is(err, talentprofile.ErrNotTalent):
		return &gqlerror.Error{Message: "forbidden: no talent profile", Extensions: map[string]any{"code": "FORBIDDEN"}}
	default:
		return err
	}
}

func toUpdateBasicsInput(in model.UpdateTalentProfileInput) talentprofile.UpdateBasicsInput {
	return talentprofile.UpdateBasicsInput{
		Headline:      in.Headline,
		Bio:           in.Bio,
		HomePlaceCode: in.HomePlaceCode,
		GenreCodes:    in.GenreCodes,
		TypeCodes:     in.TypeCodes,
		LanguageCodes: in.LanguageCodes,
		ServiceAreas:  in.ServiceAreaCodes,
	}
}

func toRateInputs(in []*model.RateInput) []talentprofile.RateInput {
	out := make([]talentprofile.RateInput, 0, len(in))
	for _, r := range in {
		out = append(out, talentprofile.RateInput{
			Amount: r.Amount, CurrencyCode: r.CurrencyCode, RateUnitCode: r.RateUnitCode, EventTypeCode: r.EventTypeCode,
		})
	}
	return out
}

func toDomainSettlementType(t model.SettlementType) payments.SettlementType {
	if t == model.SettlementTypeMobileMoney {
		return payments.SettlementMobileMoney
	}
	return payments.SettlementBank
}

func toConnectPayoutInput(in model.ConnectPayoutInput) talentprofile.ConnectPayoutInput {
	return talentprofile.ConnectPayoutInput{
		BusinessName:   in.BusinessName,
		SettlementType: toDomainSettlementType(in.SettlementType),
		BankCode:       in.BankCode,
		AccountNumber:  in.AccountNumber,
	}
}

func toModelPayoutAccount(a *talentprofile.PayoutAccount) *model.PayoutAccount {
	st := model.SettlementTypeBank
	if a.SettlementType == string(payments.SettlementMobileMoney) {
		st = model.SettlementTypeMobileMoney
	}
	return &model.PayoutAccount{
		SettlementType:     st,
		BankName:           a.BankName,
		AccountNumberLast4: a.AccountNumberLast4,
		AccountName:        a.AccountName,
	}
}

func mapVerificationErr(err error) error {
	switch {
	case errors.Is(err, verification.ErrUnknownCode):
		return &gqlerror.Error{Message: err.Error(), Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	case errors.Is(err, verification.ErrNotFound):
		return &gqlerror.Error{Message: "verification not found", Extensions: map[string]any{"code": "NOT_FOUND"}}
	case errors.Is(err, verification.ErrNotPending):
		return &gqlerror.Error{Message: "already reviewed", Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	default:
		return err
	}
}

func toModelVerification(v *verification.Verification) *model.Verification {
	return &model.Verification{
		ID: v.ID, TypeCode: v.TypeCode, TypeName: v.TypeName,
		StatusCode: v.StatusCode, StatusName: v.StatusName,
		EvidenceRef: v.EvidenceRef, Notes: v.Notes, ReviewNotes: v.ReviewNotes,
	}
}

func mapAvailabilityErr(err error) error {
	switch {
	case errors.Is(err, availability.ErrInvalidRange), errors.Is(err, availability.ErrInvalidInput):
		return &gqlerror.Error{Message: err.Error(), Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	case errors.Is(err, availability.ErrNotFound):
		return &gqlerror.Error{Message: "availability block not found", Extensions: map[string]any{"code": "NOT_FOUND"}}
	case errors.Is(err, availability.ErrNotTalent):
		return &gqlerror.Error{Message: "forbidden: no talent profile", Extensions: map[string]any{"code": "FORBIDDEN"}}
	default:
		return err
	}
}

// parseRange parses the ISO 8601 `from`/`to` strings GraphQL clients send.
func parseRange(from, to string) (time.Time, time.Time, error) {
	f, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: invalid `from` date", availability.ErrInvalidInput)
	}
	t, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: invalid `to` date", availability.ErrInvalidInput)
	}
	return f, t, nil
}

func toAddBlockInput(in model.AddAvailabilityBlockInput) (availability.AddBlockInput, error) {
	starts, ends, err := parseRange(in.StartsAt, in.EndsAt)
	if err != nil {
		return availability.AddBlockInput{}, err
	}
	return availability.AddBlockInput{StartsAt: starts, EndsAt: ends, IsAvailable: in.IsAvailable, Note: in.Note}, nil
}

func toModelAvailabilityBlock(b *availability.Block) *model.AvailabilityBlock {
	return &model.AvailabilityBlock{
		ID: b.ID, StartsAt: b.StartsAt.Format(time.RFC3339), EndsAt: b.EndsAt.Format(time.RFC3339),
		IsAvailable: b.IsAvailable, Note: b.Note,
	}
}

func mapBookingErr(err error) error {
	switch {
	case errors.Is(err, booking.ErrInvalidInput), errors.Is(err, booking.ErrRateNotFound),
		errors.Is(err, booking.ErrTalentNotBookable), errors.Is(err, booking.ErrNotAvailable),
		errors.Is(err, booking.ErrConflict), errors.Is(err, booking.ErrNotPending),
		errors.Is(err, booking.ErrNotYourTurn), errors.Is(err, booking.ErrMaxRounds):
		return &gqlerror.Error{Message: err.Error(), Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	case errors.Is(err, booking.ErrNotFound):
		return &gqlerror.Error{Message: "booking not found", Extensions: map[string]any{"code": "NOT_FOUND"}}
	case errors.Is(err, booking.ErrForbidden):
		return &gqlerror.Error{Message: "forbidden", Extensions: map[string]any{"code": "FORBIDDEN"}}
	default:
		return err
	}
}

func toRequestInput(in model.RequestBookingInput) (booking.RequestInput, error) {
	startsAt, endsAt, err := parseRange(in.StartsAt, in.EndsAt)
	if err != nil {
		return booking.RequestInput{}, err
	}
	return booking.RequestInput{
		TalentID: in.TalentID, EventTypeCode: in.EventTypeCode, PlaceCode: in.PlaceCode,
		StartsAt: startsAt, EndsAt: endsAt, VenueText: in.VenueText, RateUnitCode: in.RateUnitCode, Note: in.Note,
	}, nil
}

func toModelOrganizerBooking(b *booking.OrganizerBookingItem) *model.OrganizerBooking {
	return &model.OrganizerBooking{
		ID: b.ID, StatusCode: b.StatusCode, StatusName: b.StatusName,
		StartsAt: b.StartsAt.Format(time.RFC3339), EndsAt: b.EndsAt.Format(time.RFC3339), VenueText: b.VenueText,
		QuotedAmount: b.QuotedAmount, CurrencyCode: b.CurrencyCode,
		LastOfferByRole: b.LastOfferByRole, CounterCount: b.CounterCount, AwaitingMyResponse: b.AwaitingMyResponse,
		TalentDisplayName: b.TalentDisplayName, TalentID: b.TalentID, EventTypeName: b.EventTypeName,
	}
}

func toModelTalentBooking(b *booking.TalentBookingItem) *model.TalentBooking {
	return &model.TalentBooking{
		ID: b.ID, StatusCode: b.StatusCode, StatusName: b.StatusName,
		StartsAt: b.StartsAt.Format(time.RFC3339), EndsAt: b.EndsAt.Format(time.RFC3339), VenueText: b.VenueText,
		QuotedAmount: b.QuotedAmount, CurrencyCode: b.CurrencyCode,
		LastOfferByRole: b.LastOfferByRole, CounterCount: b.CounterCount, AwaitingMyResponse: b.AwaitingMyResponse,
		OrganizerDisplayName: b.OrganizerDisplayName, OrganizerID: b.OrganizerID, EventTypeName: b.EventTypeName,
	}
}

func toModelOffers(in []booking.Offer) []*model.BookingOffer {
	out := make([]*model.BookingOffer, 0, len(in))
	for _, o := range in {
		out = append(out, &model.BookingOffer{
			ID: o.ID, Round: o.Round, ByRole: o.ByRole, ByName: o.ByName, ByMe: o.ByMe,
			Amount: o.Amount, AmountPesewas: int(o.AmountPesewas), CurrencyCode: o.CurrencyCode,
			StartsAt: o.StartsAt.Format(time.RFC3339), EndsAt: o.EndsAt.Format(time.RFC3339),
			Note: o.Note, CreatedAt: o.CreatedAt.Format(time.RFC3339),
		})
	}
	return out
}

func toCounterInput(in model.CounterBookingInput) (booking.CounterInput, error) {
	out := booking.CounterInput{BookingID: in.BookingID, AmountPesewas: int64(in.AmountPesewas), CurrencyCode: in.CurrencyCode, Note: in.Note}
	if in.StartsAt != nil || in.EndsAt != nil {
		if in.StartsAt == nil || in.EndsAt == nil {
			return out, &gqlerror.Error{Message: "provide both startsAt and endsAt, or neither", Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
		}
		starts, ends, err := parseRange(*in.StartsAt, *in.EndsAt)
		if err != nil {
			return out, err
		}
		out.StartsAt, out.EndsAt = &starts, &ends
	}
	return out, nil
}

func toModelBookingDetail(b *booking.Detail) *model.BookingDetail {
	return &model.BookingDetail{
		Offers:          toModelOffers(b.Offers),
		LastOfferByRole: b.LastOfferByRole, CounterCount: b.CounterCount, AwaitingMyResponse: b.AwaitingMyResponse,
		ID: b.ID, StatusCode: b.StatusCode, StatusName: b.StatusName,
		StartsAt: b.StartsAt.Format(time.RFC3339), EndsAt: b.EndsAt.Format(time.RFC3339), VenueText: b.VenueText,
		QuotedAmount: b.QuotedAmount, CurrencyCode: b.CurrencyCode,
		OrganizerDisplayName: b.OrganizerDisplayName, TalentDisplayName: b.TalentDisplayName,
		EventTypeName: b.EventTypeName, TermsText: b.TermsText,
	}
}

func mapCheckoutErr(err error) error {
	switch {
	case errors.Is(err, checkout.ErrInvalidInput), errors.Is(err, checkout.ErrNotAgreed),
		errors.Is(err, checkout.ErrPayoutNotReady), errors.Is(err, checkout.ErrNoPayment):
		return &gqlerror.Error{Message: err.Error(), Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	case errors.Is(err, checkout.ErrNotFound):
		return &gqlerror.Error{Message: "booking not found", Extensions: map[string]any{"code": "NOT_FOUND"}}
	case errors.Is(err, checkout.ErrForbidden):
		return &gqlerror.Error{Message: "forbidden", Extensions: map[string]any{"code": "FORBIDDEN"}}
	default:
		return err
	}
}

func toModelCheckout(c *checkout.Checkout) *model.Checkout {
	return &model.Checkout{
		CheckoutURL: c.CheckoutURL, Reference: c.Reference,
		AmountPesewas: int(c.AmountPesewas), CommissionPesewas: int(c.CommissionPesewas),
	}
}

func toModelPayment(p *checkout.PaymentStatus) *model.Payment {
	return &model.Payment{
		Reference: p.Reference, StatusCode: p.StatusCode, StatusName: p.StatusName,
		AmountPesewas: int(p.AmountPesewas), CommissionPesewas: int(p.CommissionPesewas),
		CurrencyCode: p.CurrencyCode, CheckoutURL: p.CheckoutURL,
	}
}

func mapReviewErr(err error) error {
	switch {
	case errors.Is(err, review.ErrInvalidInput), errors.Is(err, review.ErrNotCompleted), errors.Is(err, review.ErrAlreadyReviewed):
		return &gqlerror.Error{Message: err.Error(), Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	case errors.Is(err, review.ErrNotFound):
		return &gqlerror.Error{Message: "booking not found", Extensions: map[string]any{"code": "NOT_FOUND"}}
	case errors.Is(err, review.ErrForbidden):
		return &gqlerror.Error{Message: "forbidden", Extensions: map[string]any{"code": "FORBIDDEN"}}
	default:
		return err
	}
}

func toModelReview(r *review.Review) *model.Review {
	return &model.Review{
		ID: r.ID, Rating: r.Rating, Body: r.Body, CreatedAt: r.CreatedAt.Format(time.RFC3339),
		AuthorUserID: r.AuthorUserID, AuthorDisplayName: r.AuthorDisplayName,
	}
}

func toModelReviews(rs []review.Review) []*model.Review {
	out := make([]*model.Review, 0, len(rs))
	for i := range rs {
		out = append(out, toModelReview(&rs[i]))
	}
	return out
}

func toModelReviewSummary(s *review.Summary) *model.ReviewSummary {
	return &model.ReviewSummary{ReviewCount: s.ReviewCount, AverageRating: s.AverageRating}
}

func mapDisputeErr(err error) error {
	switch {
	case errors.Is(err, dispute.ErrInvalidInput), errors.Is(err, dispute.ErrNotDisputable), errors.Is(err, dispute.ErrAlreadyOpen):
		return &gqlerror.Error{Message: err.Error(), Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	case errors.Is(err, dispute.ErrNotFound):
		return &gqlerror.Error{Message: "booking not found", Extensions: map[string]any{"code": "NOT_FOUND"}}
	case errors.Is(err, dispute.ErrDisputeNotFound):
		return &gqlerror.Error{Message: "dispute not found", Extensions: map[string]any{"code": "NOT_FOUND"}}
	case errors.Is(err, dispute.ErrForbidden):
		return &gqlerror.Error{Message: "forbidden", Extensions: map[string]any{"code": "FORBIDDEN"}}
	default:
		return err
	}
}

func toModelDispute(d *dispute.Dispute) *model.Dispute {
	return &model.Dispute{
		ID: d.ID, BookingID: d.BookingID, ReasonCode: d.ReasonCode, ReasonName: d.ReasonName,
		StatusCode: d.StatusCode, StatusName: d.StatusName, Details: d.Details, ResolutionNote: d.ResolutionNote,
		OpenedByUserID: d.OpenedByUserID, CreatedAt: d.CreatedAt.Format(time.RFC3339),
	}
}

func toModelDisputes(ds []dispute.Dispute) []*model.Dispute {
	out := make([]*model.Dispute, 0, len(ds))
	for i := range ds {
		out = append(out, toModelDispute(&ds[i]))
	}
	return out
}

func toModelAdminDispute(d *dispute.AdminItem) *model.AdminDispute {
	return &model.AdminDispute{
		ID: d.ID, BookingID: d.BookingID, ReasonCode: d.ReasonCode, ReasonName: d.ReasonName,
		StatusCode: d.StatusCode, StatusName: d.StatusName, Details: d.Details, ResolutionNote: d.ResolutionNote,
		OpenedByUserID: d.OpenedByUserID, CreatedAt: d.CreatedAt.Format(time.RFC3339),
		OrganizerDisplayName: d.OrganizerDisplayName, TalentDisplayName: d.TalentDisplayName,
		BookingStatusCode: d.BookingStatusCode, StartsAt: d.StartsAt.Format(time.RFC3339),
	}
}

// adminDisputeByID re-fetches a single dispute through the admin list query
// (no single-dispute-as-admin-item query exists yet) so a mutation's
// response always matches what a follow-up list query would show.
func (r *Resolver) adminDisputeByID(ctx context.Context, id string) (*model.AdminDispute, error) {
	rows, err := r.Disputes.ListForAdmin(ctx, nil)
	if err != nil {
		return nil, mapDisputeErr(err)
	}
	for i := range rows {
		if rows[i].ID == id {
			return toModelAdminDispute(&rows[i]), nil
		}
	}
	return nil, mapDisputeErr(dispute.ErrDisputeNotFound)
}

// bookingDetail assembles the full BookingDetail — the booking itself, its
// latest payment (if any), and its reviews (if any) — the same shape
// whether reached via the booking query or a mutation's response.
func (r *Resolver) bookingDetail(ctx context.Context, callerUserID, bookingID string) (*model.BookingDetail, error) {
	detail, err := r.Bookings.GetDetail(ctx, callerUserID, bookingID)
	if err != nil {
		return nil, mapBookingErr(err)
	}
	out := toModelBookingDetail(detail)
	if payment, err := r.Checkout.LatestPayment(ctx, callerUserID, bookingID); err == nil {
		out.Payment = toModelPayment(payment)
	} else if !errors.Is(err, checkout.ErrNoPayment) {
		return nil, mapCheckoutErr(err)
	}
	reviews, err := r.Reviews.ListForBooking(ctx, callerUserID, bookingID)
	if err != nil {
		return nil, mapReviewErr(err)
	}
	out.Reviews = toModelReviews(reviews)
	disputes, err := r.Disputes.ListForBooking(ctx, callerUserID, bookingID)
	if err != nil {
		return nil, mapDisputeErr(err)
	}
	out.Disputes = toModelDisputes(disputes)
	return out, nil
}

// organizerBookingByID/talentBookingByID re-fetch through the list queries
// (no single-booking-as-item query exists yet) so a mutation's response
// always matches what a follow-up list query would show.
func (r *Resolver) organizerBookingByID(ctx context.Context, id string) (*model.OrganizerBooking, error) {
	p, _ := gqlauth.FromContext(ctx)
	rows, err := r.Bookings.ListMyBookingsAsOrganizer(ctx, p.UserID)
	if err != nil {
		return nil, mapBookingErr(err)
	}
	for _, row := range rows {
		if row.ID == id {
			row := row
			return toModelOrganizerBooking(&row), nil
		}
	}
	return nil, mapBookingErr(booking.ErrNotFound)
}

func (r *Resolver) talentBookingByID(ctx context.Context, userID, id string) (*model.TalentBooking, error) {
	rows, err := r.Bookings.ListMyBookingsAsTalent(ctx, userID)
	if err != nil {
		return nil, mapBookingErr(err)
	}
	for _, row := range rows {
		if row.ID == id {
			row := row
			return toModelTalentBooking(&row), nil
		}
	}
	return nil, mapBookingErr(booking.ErrNotFound)
}

func toDiscoverySort(s *model.TalentSort) discovery.Sort {
	if s == nil {
		return discovery.SortNewest
	}
	switch *s {
	case model.TalentSortTopRated:
		return discovery.SortTopRated
	case model.TalentSortFeatured:
		return discovery.SortFeatured
	}
	return discovery.SortNewest
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func mapTalentRequestErr(err error) error {
	switch {
	case errors.Is(err, talentrequest.ErrInvalidInput):
		return &gqlerror.Error{Message: err.Error(), Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	case errors.Is(err, talentrequest.ErrNotFound):
		return &gqlerror.Error{Message: "request not found", Extensions: map[string]any{"code": "NOT_FOUND"}}
	default:
		return err
	}
}

func toModelTalentRequest(r *talentrequest.Request) *model.TalentRequest {
	return &model.TalentRequest{
		ID: r.ID, StatusCode: r.StatusCode, StatusName: r.StatusName,
		EventTypeCode: r.EventTypeCode, EventTypeName: r.EventTypeName,
		GenreCode: r.GenreCode, GenreName: r.GenreName, PlaceCode: r.PlaceCode, PlaceName: r.PlaceName,
		EventDate: r.EventDate, BudgetPesewas: r.BudgetPesewas, CurrencyCode: r.CurrencyCode, Note: r.Note,
		CreatedAt: r.CreatedAt.Format(time.RFC3339), OrganizerDisplayName: r.OrganizerDisplayName,
	}
}

func toModelTalentRequests(rs []talentrequest.Request) []*model.TalentRequest {
	out := make([]*model.TalentRequest, 0, len(rs))
	for i := range rs {
		out = append(out, toModelTalentRequest(&rs[i]))
	}
	return out
}

func mapOrganizerProfileErr(err error) error {
	switch {
	case errors.Is(err, organizerprofile.ErrInvalidInput), errors.Is(err, organizerprofile.ErrUnknownCode):
		return &gqlerror.Error{Message: err.Error(), Extensions: map[string]any{"code": "BAD_USER_INPUT"}}
	default:
		return err
	}
}

func toModelOrganizerProfile(p *organizerprofile.Profile) *model.OrganizerProfile {
	tags := make([]*model.Tag, 0, len(p.EventTypes))
	for _, t := range p.EventTypes {
		tags = append(tags, &model.Tag{Code: t.Code, Name: t.Name})
	}
	return &model.OrganizerProfile{
		DisplayName: p.DisplayName, Organization: p.Organization, Phone: p.Phone,
		HomePlaceCode: p.HomePlaceCode, HomePlaceName: p.HomePlaceName, EventTypes: tags,
	}
}
