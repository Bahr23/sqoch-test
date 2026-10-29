package domain_test

import (
	"errors"
	"sqoch-ticket/backend/internal/domain"
	"testing"
)

func input(ages ...int) domain.OrderInput {
	p := make([]domain.ParticipantInput, len(ages))
	for i, age := range ages {
		p[i] = domain.ParticipantInput{Name: " Анна ", Age: &age}
	}
	return domain.OrderInput{Email: "anna@example.ru", Participants: p}
}
func TestPriceAndAccompanimentBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ages    []int
		total   int
		invalid string
	}{
		{"adult", []int{18}, 2000, ""},
		{"infant with adult", []int{0, 18}, 3000, ""},
		{"last child age", []int{13, 18}, 3000, ""},
		{"first full fare age", []int{14, 18}, 4000, ""},
		{"seventeen still needs adult", []int{17}, 0, "accompaniment"},
		{"four valid people", []int{13, 14, 17, 18}, 7000, ""},
		{"child with minor is invalid", []int{13, 17}, 0, "accompaniment"},
		{"full fare minor alone", []int{14}, 0, "accompaniment"},
		{"no people", nil, 0, "participants"},
		{"five people", []int{18, 18, 18, 18, 18}, 0, "participants"},
		{"negative age", []int{-1}, 0, "participants.0.age"},
		{"age above maximum", []int{121}, 0, "participants.0.age"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			people, total, err := domain.PriceOrder(input(tc.ages...), domain.Event{FullPrice: 2000, ChildPrice: 1000})
			if tc.invalid != "" {
				var invalid *domain.ValidationError
				if !errors.As(err, &invalid) || invalid.Fields[tc.invalid] == "" {
					t.Fatalf("expected %s, got %v", tc.invalid, err)
				}
				return
			}
			if err != nil || total != tc.total {
				t.Fatalf("total=%d error=%v", total, err)
			}
			if people[0].Name != "Анна" {
				t.Fatal("name was not normalized")
			}
		})
	}
}
func TestMissingAndInvalidFields(t *testing.T) {
	req := domain.OrderInput{Email: "not an email", Participants: []domain.ParticipantInput{{Name: "   "}}}
	_, _, err := domain.PriceOrder(req, domain.Event{FullPrice: 2000, ChildPrice: 1000})
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatal(err)
	}
	for _, key := range []string{"email", "participants.0.name", "participants.0.age"} {
		if invalid.Fields[key] == "" {
			t.Errorf("missing error for %s", key)
		}
	}
}
