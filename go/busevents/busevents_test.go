package busevents

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var at = time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)

func TestSubjects(t *testing.T) {
	s := Subject("synthiz", AccountActivated)
	if s != "events.synthiz.account.activated" {
		t.Fatalf("Subject = %q", s)
	}
	if ProductOf(s) != "synthiz" || NameOf(s) != AccountActivated {
		t.Fatalf("ProductOf/NameOf(%q) = %q, %q", s, ProductOf(s), NameOf(s))
	}
	if ProductOf("finance.subscription.activated") != "" {
		t.Fatal("a subject outside events. has no product")
	}
}

func TestPayloadsAreFlat(t *testing.T) {
	body, id, err := Encode(SignedUp{Meta: Meta{EventID: "ev-1", OccurredAt: at}, PersonID: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if id != "ev-1" {
		t.Fatalf("msg id = %q", id)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["event_id"] != "ev-1" || raw["person_id"] != "p1" || raw["occurred_at"] == nil {
		t.Fatalf("event_id, occurred_at and person_id must sit at the top level: %s", body)
	}
	if strings.Contains(string(body), "Meta") {
		t.Fatalf("no envelope: %s", body)
	}
}

func TestEncodeRefusesAnIncompleteEvent(t *testing.T) {
	cases := []Event{
		SignedUp{Meta: Meta{OccurredAt: at}, PersonID: "p"},
		SignedUp{Meta: Meta{EventID: "e"}, PersonID: "p"},
		SignedUp{Meta: Meta{EventID: "e", OccurredAt: at}},
		Activated{Meta: Meta{EventID: "e", OccurredAt: at}, PersonID: "p"},
		Subscription{Meta: Meta{EventID: "e", OccurredAt: at}, AppSlug: "spore", PersonID: "p", SubscriptionID: "s", Interval: "week"},
		DailyVisitors{Meta: Meta{EventID: "e", OccurredAt: at}, AppSlug: "spore", Date: "26/09"},
		DeletionRequested{EventID: "e", Product: "spore"},
	}
	for _, e := range cases {
		if _, _, err := Encode(e); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: err = %v; want ErrInvalid", e, err)
		}
	}
}

func TestSubscriptionRoundTrip(t *testing.T) {
	end := at.AddDate(0, 1, 0)
	sent := Subscription{
		Meta: Meta{EventID: "ev-2", OccurredAt: at}, AppSlug: "synthiz", PersonID: "p2",
		SubscriptionID: "sub-1", AmountCents: 12000, Currency: "EUR", Interval: Yearly,
		EffectiveAt: &end, CancelAtPeriodEnd: true,
	}
	body, _, err := Encode(sent)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode[Subscription](body)
	if err != nil {
		t.Fatal(err)
	}
	if got.MonthlyCents() != 1000 || !got.EndsAt().Equal(end) || !got.CancelAtPeriodEnd {
		t.Fatalf("decoded %+v", got)
	}
}

func TestEndsAtDefaultsToOccurredAt(t *testing.T) {
	s := Subscription{Meta: Meta{OccurredAt: at}}
	if !s.EndsAt().Equal(at) {
		t.Fatalf("EndsAt = %s", s.EndsAt())
	}
}

func TestDecodeRefusesGarbage(t *testing.T) {
	if _, err := Decode[SignedUp]([]byte("not json")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if _, err := Decode[SignedUp]([]byte(`{"event_id":"e","occurred_at":"2026-09-26T10:00:00Z"}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("event without a person accepted: %v", err)
	}
}

func TestUrbangatePayloadMatchesItsPublisher(t *testing.T) {
	body := []byte(`{"event_id":"e","identity_id":"id-1","product":"synthiz","user_id":"u1","email":"x@y.z"}`)
	got, err := Decode[DeletionRequested](body)
	if err != nil {
		t.Fatal(err)
	}
	if got.IdentityID != "id-1" || got.Product != "synthiz" || got.UserID != "u1" {
		t.Fatalf("decoded %+v", got)
	}
}
