package sdk

import (
	"net/http"
	"testing"
	"time"
)

const introCatalogueBody = `{"plans":[
	{"id":"p-solo","code":"solo","amount":2900,"currency":"EUR","interval":"month","interval_count":1,"rank":10,
	 "intro":{"amount_cents":1900,"periods":12,"available_until":"2027-01-31T23:00:00Z"}},
	{"id":"p-team","code":"team","amount":9900,"currency":"EUR","interval":"month","interval_count":1,"rank":20,
	 "intro":{"amount_cents":4900,"periods":3,"available_until":null}},
	{"id":"p-pro","code":"pro","amount":4900,"currency":"EUR","interval":"month","interval_count":1,"rank":30,"intro":null}
]}`

func TestListPlans_ReadsTheIntroPrice(t *testing.T) {
	srv, rec := server(t, http.StatusOK, nil)
	defer srv.Close()
	srv.Config.Handler = jsonHandler(introCatalogueBody, rec)

	plans, err := New(srv.URL, "key").ListPlans(ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	solo, _ := plans.ByCode("solo")
	if solo.Intro == nil {
		t.Fatal("solo has no intro")
	}
	until := time.Date(2027, 1, 31, 23, 0, 0, 0, time.UTC)
	if solo.Intro.AmountCents != 1900 || solo.Intro.Periods != 12 {
		t.Fatalf("solo intro = %+v, want 1900 x 12", *solo.Intro)
	}
	if solo.Intro.AvailableUntil == nil || !solo.Intro.AvailableUntil.Equal(until) {
		t.Fatalf("solo intro available until = %v, want %s", solo.Intro.AvailableUntil, until)
	}

	team, _ := plans.ByCode("team")
	if team.Intro == nil || team.Intro.AmountCents != 4900 || team.Intro.Periods != 3 {
		t.Fatalf("team intro = %+v, want 4900 x 3", team.Intro)
	}
	if team.Intro.AvailableUntil != nil {
		t.Fatalf("team intro deadline = %v, want none", team.Intro.AvailableUntil)
	}

	pro, _ := plans.ByCode("pro")
	if pro.Intro != nil {
		t.Fatalf("pro intro = %+v, want nil", *pro.Intro)
	}
}

func TestListPlans_NoIntroFieldMeansNoIntro(t *testing.T) {
	srv, rec := server(t, http.StatusOK, nil)
	defer srv.Close()
	srv.Config.Handler = jsonHandler(catalogueBody, rec)

	plans, err := New(srv.URL, "key").ListPlans(ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, p := range plans {
		if p.Intro != nil {
			t.Fatalf("%s intro = %+v, want nil", p.Code, *p.Intro)
		}
	}
}

func TestEntitlement_CarriesTheIntroPrice(t *testing.T) {
	srv, _ := server(t, http.StatusOK, map[string]any{
		"entitled": true, "status": "active",
		"intro_amount_cents": 1900, "intro_periods_remaining": 4,
	})

	got, err := New(srv.URL, "k").Entitlement(ctx(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.IntroAmountCents != 1900 || got.IntroPeriodsRemaining != 4 {
		t.Fatalf("intro = %d x %d, want 1900 x 4", got.IntroAmountCents, got.IntroPeriodsRemaining)
	}
}

func TestEntitlement_ListPriceHasNoIntro(t *testing.T) {
	srv, _ := server(t, http.StatusOK, map[string]any{"entitled": true, "status": "active"})

	got, err := New(srv.URL, "k").Entitlement(ctx(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.IntroAmountCents != 0 || got.IntroPeriodsRemaining != 0 {
		t.Fatalf("intro = %d x %d, want none", got.IntroAmountCents, got.IntroPeriodsRemaining)
	}
}

const introEndingDelivery = `{"type":"subscription.intro_ending","id":"sub_1:active:2026-12-01T00:00:00Z:intro_ending",
"createdAt":"2026-11-01T00:00:00Z","data":{
	"SubscriptionID":"7c1d6a4e-0000-4000-8000-000000000001","TenantID":"7c1d6a4e-0000-4000-8000-000000000002",
	"CustomerID":"7c1d6a4e-0000-4000-8000-000000000003","AppID":"7c1d6a4e-0000-4000-8000-000000000004",
	"ExternalUserID":"user-1","ProductCode":"solo","Status":"active",
	"CurrentPeriodStart":"2026-11-01T00:00:00Z","CurrentPeriodEnd":"2026-12-01T00:00:00Z",
	"IntroAmountCents":1900,"IntroPeriodsRemaining":0,"OccurredAt":"2026-11-01T00:00:00Z",
	"NextAmountCents":2900,"Currency":"EUR","SwitchAt":"2026-12-01T00:00:00Z"}}`

func TestDelivery_DecodesIntroEnding(t *testing.T) {
	d := Delivery{Type: EventSubscriptionIntroEnding, Payload: []byte(introEndingDelivery)}

	got, err := d.SubscriptionIntroEnding()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	switchAt := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if got.NextAmountCents != 2900 || got.Currency != "EUR" || !got.SwitchAt.Equal(switchAt) {
		t.Fatalf("next = %d %s at %s, want 2900 EUR at %s", got.NextAmountCents, got.Currency, got.SwitchAt, switchAt)
	}
	if got.ExternalUserID != "user-1" || got.SubscriptionID != "7c1d6a4e-0000-4000-8000-000000000001" {
		t.Fatalf("subject = %q / %q", got.ExternalUserID, got.SubscriptionID)
	}
	if got.IntroAmountCents != 1900 || got.IntroPeriodsRemaining != 0 {
		t.Fatalf("intro = %d x %d, want 1900 x 0", got.IntroAmountCents, got.IntroPeriodsRemaining)
	}
	if got.CurrentPeriodEnd == nil || !got.CurrentPeriodEnd.Equal(switchAt) {
		t.Fatalf("period end = %v, want %s", got.CurrentPeriodEnd, switchAt)
	}
}

func TestDelivery_DecodesTheIntroOnEverySubscriptionEvent(t *testing.T) {
	body := `{"type":"subscription.renewed","data":{"SubscriptionID":"sub_1","Status":"active",
		"IntroAmountCents":1900,"IntroPeriodsRemaining":5}}`
	d := Delivery{Type: EventSubscriptionRenewed, Payload: []byte(body)}

	got, err := d.SubscriptionEvent()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.IntroAmountCents != 1900 || got.IntroPeriodsRemaining != 5 {
		t.Fatalf("intro = %d x %d, want 1900 x 5", got.IntroAmountCents, got.IntroPeriodsRemaining)
	}
}

func TestDelivery_RefusesTheWrongEventType(t *testing.T) {
	if _, err := (Delivery{Type: EventSubscriptionRenewed, Payload: []byte(introEndingDelivery)}).SubscriptionIntroEnding(); err == nil {
		t.Fatal("decoded a renewal as an intro ending")
	}
	if _, err := (Delivery{Type: EventEntitlementChanged, Payload: []byte(`{"data":{}}`)}).SubscriptionEvent(); err == nil {
		t.Fatal("decoded an entitlement change as a subscription event")
	}
}

func TestDelivery_RefusesADeliveryWithoutData(t *testing.T) {
	if _, err := (Delivery{Type: EventSubscriptionActivated, Payload: []byte(`{"type":"subscription.activated"}`)}).SubscriptionEvent(); err == nil {
		t.Fatal("decoded a delivery with no data")
	}
}
